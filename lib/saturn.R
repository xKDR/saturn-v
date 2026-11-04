# saturn helpers for R. Load with: source(Sys.getenv("SATURN_R"))
#
#   con <- saturn_db("hhd")
#   df  <- DBI::dbGetQuery(con, "SELECT year, AVG(tot_inc) AS avg_income FROM hh_income_monthly WHERE state = 'Karnataka' GROUP BY year")
#   DBI::dbListTables(con)
#
# The datasets are queried through Athena, never read directly: each query runs in AWS under
# your name and is recorded, and only its result comes back. Works the same on a laptop
# (signed in with `saturn login`) and in a job (with the job's own keys). Needs the paws and
# arrow packages: install.packages(c("paws", "arrow")); with renv, keep library(paws) and
# library(arrow) at the top of the script so renv::snapshot() records them. The SQL is
# Athena's (Trino); see the guide.

suppressPackageStartupMessages(library(DBI))

setClass("SaturnAthena", contains = "DBIConnection", slots = c(dataset = "character", state = "environment"))

saturn_db <- function(dataset = "hhd") {
  for (p in c("paws.analytics", "paws.storage", "paws.security.identity", "arrow")) {
    if (!requireNamespace(p, quietly = TRUE)) stop("saturn_db() needs the paws and arrow packages: install.packages(c(\"paws\", \"arrow\"))", call. = FALSE)
  }
  workgroup <- Sys.getenv("SATURN_ATHENA_WORKGROUP")
  if (!nzchar(workgroup)) stop("SATURN_ATHENA_WORKGROUP is not set: on a laptop run `saturn update`, then `saturn connect`", call. = FALSE)
  state <- new.env()
  state$workgroup <- workgroup
  state$bucket <- Sys.getenv("SATURN_ATHENA_RESULTS")
  state$region <- Sys.getenv("AWS_REGION", "ap-south-1")
  state$profile <- Sys.getenv("SATURN_AWS_PROFILE", "saturn-v")
  # Query results go to a folder only you can read: unload/<your AWS user id>/.
  state$userid <- paws.security.identity::sts(config = .saturn_config(state))$get_caller_identity()$UserId
  con <- new("SaturnAthena", dataset = dataset, state = state)
  tables <- DBI::dbListTables(con)
  if (length(tables) == 0) stop("no tables in '", dataset, "' that you may query", call. = FALSE)
  message(sprintf("%s: %d tables (%s)", dataset, length(tables),
                  if (length(tables) <= 10) paste(tables, collapse = ", ") else "dbListTables(con) lists them"))
  con
}

# AWS keys. A job has its own; on a laptop they come from the saturn sign-in through the AWS
# CLI (R's AWS SDK can't read the sign-in itself) and are fetched again before they expire.
.saturn_config <- function(state) {
  if (nzchar(Sys.getenv("AWS_ACCESS_KEY_ID")) || nzchar(Sys.getenv("AWS_CONTAINER_CREDENTIALS_RELATIVE_URI"))) {
    return(list(region = state$region))
  }
  if (is.null(state$keys) || state$keys$expires < Sys.time() + 300) {
    out <- suppressWarnings(system2("aws", c("configure", "export-credentials", "--profile", state$profile, "--format", "process"),
                                    stdout = TRUE, stderr = FALSE))
    if (!is.null(attr(out, "status"))) stop("not signed in to AWS: run `saturn login`", call. = FALSE)
    k <- jsonlite::fromJSON(paste(out, collapse = ""))
    state$keys <- list(creds = list(access_key_id = k$AccessKeyId, secret_access_key = k$SecretAccessKey, session_token = k$SessionToken),
                       expires = as.POSIXct(sub("(\\+00:00|Z)$", "", k$Expiration), format = "%Y-%m-%dT%H:%M:%S", tz = "UTC"))
  }
  list(region = state$region, credentials = list(creds = state$keys$creds))
}

# Runs one query in Athena and returns its result as a data frame. A SELECT comes back as
# Parquet (UNLOAD: typed, and quick even for large results); one that sorts (ORDER BY), or
# isn't a SELECT, comes back as Athena's CSV, since UNLOAD may split the rows over files.
.saturn_query <- function(con, sql) {
  st <- con@state
  config <- .saturn_config(st)
  athena <- paws.analytics::athena(config = config)
  s3 <- paws.storage::s3(config = config)
  sql <- sub(";\\s*$", "", trimws(sql))
  unload <- grepl("^(select|with)\\b", sql, ignore.case = TRUE) && !grepl("\\border\\s+by\\b", sql, ignore.case = TRUE)
  prefix <- sprintf("unload/%s/%s-%d/", st$userid, format(Sys.time(), "%Y%m%dT%H%M%OS6"), Sys.getpid())
  query <- if (unload) sprintf("UNLOAD (%s) TO 's3://%s/%s' WITH (format = 'PARQUET', compression = 'ZSTD')", sql, st$bucket, prefix) else sql
  id <- athena$start_query_execution(QueryString = query, WorkGroup = st$workgroup,
                                     QueryExecutionContext = list(Database = con@dataset))$QueryExecutionId
  repeat {
    q <- athena$get_query_execution(QueryExecutionId = id)$QueryExecution
    if (q$Status$State %in% c("SUCCEEDED", "FAILED", "CANCELLED")) break
    Sys.sleep(0.3)
  }
  if (q$Status$State != "SUCCEEDED") stop("query ", tolower(q$Status$State), ": ", q$Status$StateChangeReason, call. = FALSE)
  read <- function(bucket, key, reader) reader(arrow::BufferReader$create(s3$get_object(Bucket = bucket, Key = key)$Body))
  if (unload) {
    keys <- vapply(s3$list_objects_v2(Bucket = st$bucket, Prefix = prefix)$Contents, function(o) o$Key, "")
    if (length(keys) == 0) return(data.frame())
    parts <- lapply(keys, read, bucket = st$bucket, reader = function(f) arrow::read_parquet(f, as_data_frame = FALSE))
    return(as.data.frame(do.call(arrow::concat_tables, parts)))
  }
  where <- regmatches(q$ResultConfiguration$OutputLocation, regexec("^s3://([^/]+)/(.+)$", q$ResultConfiguration$OutputLocation))[[1]]
  as.data.frame(read(where[2], where[3], function(f) arrow::read_csv_arrow(f, as_data_frame = FALSE)))
}

setMethod("dbGetQuery", signature("SaturnAthena", "character"), function(conn, statement, ...) .saturn_query(conn, statement))

setMethod("dbListTables", "SaturnAthena", function(conn, ...) {
  glue <- paws.analytics::glue(config = .saturn_config(conn@state))
  # Glue lists at most 100 tables at a time (fld has more).
  names <- character()
  token <- NULL
  repeat {
    page <- glue$get_tables(DatabaseName = conn@dataset, NextToken = token)
    names <- c(names, vapply(page$TableList, function(t) t$Name, ""))
    token <- page$NextToken
    if (length(token) == 0 || !nzchar(token)) break
  }
  sort(names)
})

setMethod("dbDisconnect", "SaturnAthena", function(conn, ...) invisible(TRUE))
setMethod("dbIsValid", "SaturnAthena", function(dbObj, ...) TRUE)
setMethod("show", "SaturnAthena", function(object) cat("<saturn: Athena, dataset ", object@dataset, ">\n", sep = ""))

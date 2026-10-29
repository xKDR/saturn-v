# saturn helpers for R. Load with: source(Sys.getenv("SATURN_R"))
#
#   con <- saturn_db("hhd")    # DuckDB with every hhd table as a view
#   dbGetQuery(con, "SELECT ... FROM hh_income_monthly WHERE STATE = 'Karnataka'")
#
# Works the same on a laptop (after `saturn connect`, DATASETS is a local folder) and in
# a job (DATASETS is s3://...). Needs the duckdb package.

saturn_db <- function(dataset, con = NULL) {
  datasets <- Sys.getenv("DATASETS")
  if (!nzchar(datasets)) stop("DATASETS is not set: run `saturn connect` first (on a laptop), or run the script with `saturn run`")
  root <- paste0(sub("/+$", "", datasets), "/", dataset)
  if (is.null(con)) con <- DBI::dbConnect(duckdb::duckdb())

  if (startsWith(root, "s3://")) {
    DBI::dbExecute(con, "INSTALL httpfs")
    DBI::dbExecute(con, "LOAD httpfs")
    DBI::dbExecute(con, sprintf(
      "CREATE OR REPLACE SECRET saturn (TYPE s3, KEY_ID '%s', SECRET '%s', SESSION_TOKEN '%s', REGION '%s')",
      Sys.getenv("AWS_ACCESS_KEY_ID"), Sys.getenv("AWS_SECRET_ACCESS_KEY"),
      Sys.getenv("AWS_SESSION_TOKEN"), Sys.getenv("AWS_REGION")))
    files <- DBI::dbGetQuery(con, sprintf("SELECT file FROM glob('%s/*/**/*.parquet')", root))$file
    tables <- unique(sub("/.*", "", substring(files, nchar(root) + 2)))
  } else {
    if (!dir.exists(root)) stop("no dataset '", dataset, "' in ", datasets)
    tables <- list.dirs(root, recursive = FALSE, full.names = FALSE)
  }
  tables <- sort(tables[!startsWith(tables, "_") & !startsWith(tables, ".")])
  if (length(tables) == 0) stop("no tables found in ", root)

  for (t in tables) {
    DBI::dbExecute(con, sprintf(
      "CREATE OR REPLACE VIEW \"%s\" AS SELECT * FROM read_parquet('%s/%s/**/*.parquet', hive_partitioning = true)",
      t, root, t))
  }
  message(sprintf("%s: %d tables (%s)", dataset, length(tables), paste(tables, collapse = ", ")))
  invisible(con)
}

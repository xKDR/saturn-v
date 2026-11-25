# saturn helpers for R. Load with: source(Sys.getenv("SATURN_R"))
#
#   con <- saturn_db("hhd")    # the hhd database in mayin-data (MariaDB)
#   df  <- DBI::dbGetQuery(con, "SELECT YEAR, AVG(TOT_INC) FROM hh_income_monthly WHERE STATE = 'Karnataka' GROUP BY YEAR")
#   dplyr::tbl(con, "hh_income_monthly") |> dplyr::filter(STATE == "Karnataka") |> dplyr::collect()
#   DBI::dbListTables(con)
#   DBI::dbDisconnect(con)
#
# Works the same on a laptop (through `saturn connect`'s tunnel) and in a job: both have
# DB_HOST, DB_PORT, DB_USER and DB_PASSWORD. Needs the RMariaDB package:
# install.packages("RMariaDB"); with renv, keep library(RMariaDB) at the top of the script
# so renv::snapshot() records it.

saturn_db <- function(dataset = "hhd") {
  if (!requireNamespace("RMariaDB", quietly = TRUE)) {
    stop("saturn_db() needs the RMariaDB package: install.packages(\"RMariaDB\")", call. = FALSE)
  }
  host <- Sys.getenv("DB_HOST")
  if (!nzchar(host)) {
    stop("DB_HOST is not set: on a laptop run `saturn connect` (in RStudio: readRenviron(\"~/.config/saturn-v/env\")); ",
         "in a job it is set for you", call. = FALSE)
  }
  DBI::dbConnect(RMariaDB::MariaDB(),
                 host = host, port = as.integer(Sys.getenv("DB_PORT", "3306")),
                 user = Sys.getenv("DB_USER"), password = Sys.getenv("DB_PASSWORD"),
                 dbname = dataset,
                 # COUNT(*) etc. as plain numbers: the default (integer64) prints as 6.99e-318
                 # in cat() and paste() unless bit64 is loaded.
                 bigint = "numeric",
                 client.flag = RMariaDB::CLIENT_SSL) # the database accepts encrypted connections only
}

# A first job. The same file runs on your laptop (after `saturn connect`) and on AWS:
#
#   Rscript analysis.R                               # locally, from this folder
#   saturn run saturn-v-examples/r/analysis.R        # on AWS (path relative to the workspace)
#
# Both places have the same environment variables: WORKSPACE, SATELLITE, DATA_BUCKET,
# AWS_REGION and DB_HOST, DB_PORT, DB_USER, DB_PASSWORD.
#
# The AWS image has terra, sf, GDAL and the tidyverse. For other packages, run
# renv::init() / renv::snapshot() in this folder; jobs install what renv.lock lists.

cat(R.version.string, "on", parallel::detectCores(), "CPUs\n")
cat("Workspace:     ", Sys.getenv("WORKSPACE"), "\n")
cat("Satellite data:", Sys.getenv("SATELLITE"), "\n")   # a local folder, or /vsis3/... inside a job
cat("Database:      ", paste0(Sys.getenv("DB_USER"), "@", Sys.getenv("DB_HOST"), ":", Sys.getenv("DB_PORT")), "\n")

# Read one pixel of a satellite file (only that part is fetched); the same path works in both places.
f <- file.path(Sys.getenv("SATELLITE"), "ntl/sl/rad",
               "SVDNB_j01_20220801-20220831_global_vcmslcfg_v10_c202209231200.avg_rade9h.tif")
if (requireNamespace("terra", quietly = TRUE)) {
  v <- terra::extract(terra::rast(f), cbind(77.2, 28.6))
  cat("Radiance near Delhi, Aug 2022:", v[[1]], "\n")
}

# Database, with the same code in both places (needs RMariaDB):
#   con <- DBI::dbConnect(RMariaDB::MariaDB(), host = Sys.getenv("DB_HOST"),
#                         port = as.integer(Sys.getenv("DB_PORT")), user = Sys.getenv("DB_USER"),
#                         password = Sys.getenv("DB_PASSWORD"), dbname = "fld")

writeLines(paste("Hello at", format(Sys.time(), tz = "UTC"), "UTC from", Sys.info()[["nodename"]]), "hello.txt")
cat("Wrote hello.txt\n")

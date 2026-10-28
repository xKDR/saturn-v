# A first job: shows the machine it ran on, lists a few satellite files and writes a
# file back to the workspace.
#
#   saturn run examples/r/analysis.R            (after copying examples/ into ~/aws/workspace)
#
# The image already has terra, sf, GDAL and the tidyverse. For other packages, run
# renv::init() / renv::snapshot() in this folder; the job installs what renv.lock lists.

cat(R.version.string, "on", parallel::detectCores(), "CPUs\n")

bucket <- Sys.getenv("DATA_BUCKET")
cat("Satellite bucket:", bucket, "\n")

# terra reads straight from S3 (GDAL /vsis3/); only the parts you use are downloaded, e.g.
#   r <- terra::rast(paste0("/vsis3/", bucket, "/ntl/sl/rad/<file>.tif"))
cat("terra", as.character(packageVersion("terra")), "is available\n")

# Database (shared aws_jobs user):
#   con <- DBI::dbConnect(RMariaDB::MariaDB(), host = Sys.getenv("DB_HOST"),
#                         port = as.integer(Sys.getenv("DB_PORT")), user = Sys.getenv("DB_USER"),
#                         password = Sys.getenv("DB_PASSWORD"), dbname = "fld")

writeLines(paste("Hello from Fargate at", format(Sys.time(), tz = "UTC"), "UTC"), "hello.txt")
cat("Wrote hello.txt; it appears next to this script on your laptop shortly.\n")

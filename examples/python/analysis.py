# /// script
# requires-python = ">=3.12"
# dependencies = ["rasterio", "duckdb"]
# ///
#
# A first job. The same file runs on your laptop (after `saturn connect`) and on AWS:
#
#   uv run analysis.py                                    # locally, from this folder
#   saturn run saturn-v-examples/python/analysis.py       # on AWS (path relative to the workspace)
#
# Both places have the same environment variables: WORKSPACE, SATELLITE, DATASETS,
# DATA_BUCKET, AWS_REGION and DB_HOST, DB_PORT, DB_USER, DB_PASSWORD.
#
# The packages are listed in the header above; uv installs them (once, then cached).
# A pyproject.toml or requirements.txt in the folder works too.

import os
import platform
import sys
from datetime import datetime, timezone

print("Python", platform.python_version(), "on", os.cpu_count(), "CPUs")
print("Workspace:     ", os.environ.get("WORKSPACE"))
print("Satellite data:", os.environ.get("SATELLITE"))  # a local folder, or /vsis3/... inside a job
print("Database:      ", "%s@%s:%s" % (os.environ.get("DB_USER"), os.environ.get("DB_HOST"), os.environ.get("DB_PORT")))

# Read one pixel of a satellite file (only that part is fetched); the same path works in both places.
import rasterio

f = os.path.join(os.environ["SATELLITE"], "ntl/sl/rad",
                 "SVDNB_j01_20220801-20220831_global_vcmslcfg_v10_c202209231200.avg_rade9h.tif")
with rasterio.open(f) as r:
    print("Radiance near Delhi, Aug 2022:", next(r.sample([(77.2, 28.6)]))[0])

# The Parquet datasets, with saturn's helper.
exec(open(os.environ["SATURN_PY"]).read())
con = saturn_db("hhd")
print(con.sql("SELECT YEAR, COUNT(*) AS households FROM hh_income_monthly "
              "WHERE STATE = 'Karnataka' AND YEAR BETWEEN 2014 AND 2016 GROUP BY YEAR ORDER BY YEAR"))

with open("hello.txt", "w") as out:
    out.write("Hello at %s UTC from %s\n" % (datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S"), platform.node()))
print("Wrote hello.txt", file=sys.stderr)

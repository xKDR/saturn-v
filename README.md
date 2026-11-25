# saturn-v

[![User guide](https://img.shields.io/badge/user%20guide-xkdr.github.io%2Fsaturn--v-c2410c)](https://xkdr.github.io/saturn-v/)

**New here? Start with the [user guide](https://xkdr.github.io/saturn-v/)**: the same material as this README, with the steps in order and the code in your language (`saturn docs` opens it from your copy). This README is the full reference.

Run heavy Julia, R and Python jobs on AWS from your laptop, with the same code in both places.

You work on your laptop as usual. When a job needs more than your laptop has, `saturn run` sends it to a machine on AWS Fargate (up to 32 cores and 244 GB of memory), streams its output to your terminal, and records what it cost. Your laptop can sleep or disconnect while the job runs.

`saturn connect` gives your laptop the same setup a job has: the shared workspace, the satellite data and the [database](#the-database), under the same environment variable names. A script you try locally runs unchanged on AWS.

The AWS side (buckets, cluster, permissions) is managed in [xKDR/AWS-Server](https://github.com/xKDR/AWS-Server).

## Getting access

Ask an admin to add you to saturn-v. You'll get an email from AWS ("Invitation to join AWS IAM Identity Center"): open it within 7 days, set a password and register MFA (an authenticator app, or Touch ID / a passkey).

## Setup (once)

On a Mac or Linux, in a terminal:

```sh
curl -fsSL https://raw.githubusercontent.com/xKDR/saturn-v/main/install.sh | sh
```

Then open a new terminal. The installer:

- installs the tools saturn uses: the AWS CLI, rclone (for the mounts) and AWS's Session Manager plugin (for the [database](#the-database) tunnel). On a Mac through [Homebrew](https://brew.sh) (install that first if you don't have it); on Linux into `~/.local`, without sudo;
- puts saturn in `~/.saturn-v` and on your PATH;
- adds two lines to your shell's startup file (marked `# saturn-v`): `~/.local/bin` on the PATH, and the job environment while you're connected.

It's safe to run again. To read it first: `curl -fsSL https://raw.githubusercontent.com/xKDR/saturn-v/main/install.sh | less`.

- **Linux:** the mounts need FUSE (`sudo apt install fuse3` on Debian or Ubuntu; most desktops have it). The installer says if it's missing.
- **Updates:** when a new version is out, saturn asks you to run `saturn update` before anything else (`disconnect`, `stop` and `docs` still work). It reconnects you with the new version if you were connected.
- **Already cloned this repository?** That keeps working; `saturn update` runs `git pull` there.

## Each session

```sh
saturn login
saturn connect
```

`saturn login` opens your browser to sign in at xKDR's AWS access portal, with your password and MFA. A sign-in lasts **7 days**; saturn says when it's time to sign in again. `saturn connect` then:

- mounts the shared workspace at `~/aws/workspace`, and the satellite data, read-only, at `~/aws/satellite`;
- opens a tunnel to the [database](#the-database) on `127.0.0.1:3306`;
- creates **your own folder** in the workspace, `~/aws/workspace/<your user name>` (see [Organising the workspace](#organising-the-workspace));
- writes the job environment to `~/.config/saturn-v/env` (readable only by you), which new terminals load;
- keeps the tunnel healthy in the background, reopening it if it drops.

For the terminal you're in, use `eval "$(saturn connect)"` instead, which also sets the variables there. `saturn status` shows what's connected; `saturn disconnect` undoes it all.

Everything, the mounts included, runs on your sign-in: AWS keys are renewed in the background for the whole week, and nothing secret is stored on your laptop.

**Still have an IAM user** (signing in with `aws login --profile aws-server`, from before the switch)? That keeps working until an admin removes it; `saturn login` once moves you over.

## Organising the workspace

Everyone shares one workspace, so keep to this layout:

| Folder | For |
| --- | --- |
| `<your user name>/` (e.g. `firstname.lastname/`) | your own work. `saturn connect` creates it; `MY_WORKSPACE` points to it, on your laptop and in your jobs |
| `shared/<project>/` | work several people use; agree on the folder name with them |
| `db/`, `.julia/`, `.R/` | used by saturn itself; leave them alone |

Anything else at the top level shows up in the Monday summary as "not in anyone's folder".

In scripts, write outputs relative to `MY_WORKSPACE` (or to the script's own folder), never to a fixed path:

```r
saveRDS(results, file.path(Sys.getenv("MY_WORKSPACE"), "project", "results.rds"))
```

## The same code on your laptop and on AWS

These variables are set in every job, and on your laptop after `saturn connect`:

| Variable | In a job | On your laptop |
| --- | --- | --- |
| `WORKSPACE` | `/workspace` | `~/aws/workspace` |
| `MY_WORKSPACE` | `/workspace/<your user name>` | `~/aws/workspace/<your user name>` |
| `SATELLITE` | `/vsis3/<bucket>` (read straight from S3) | `~/aws/satellite` |
| `DATA_BUCKET`, `AWS_REGION` | the satellite bucket and AWS region | the same |
| `DB_HOST`, `DB_PORT` | the database | `127.0.0.1` and the tunnel port |
| `DB_USER`, `DB_PASSWORD` | the shared, read-only `aws_jobs` login | the same |

So build paths and connections from them, never from fixed paths:

```r
r <- terra::rast(file.path(Sys.getenv("SATELLITE"), "ntl/sl/rad/<file>.tif"))
```

```julia
using Rasters, ArchGDAL
r = Raster(joinpath(ENV["SATELLITE"], "ntl", "sl", "rad", "<file>.tif"); lazy=true)
```

```python
import os, rasterio
r = rasterio.open(os.path.join(os.environ["SATELLITE"], "ntl/sl/rad/<file>.tif"))
```

Paths relative to your script's folder also work in both places.

Reading satellite files on your laptop downloads them (about $0.11/GB), so try things on a few files or a small area there, and do the heavy reading in a job, where it's free.

RStudio and other apps started from the Dock (or a desktop launcher) don't read your shell's startup file. In R, load the environment with `readRenviron("~/.config/saturn-v/env")`.

## Running a job

Put your script in your own folder in the workspace, for example `~/aws/workspace/<you>/project/`, then:

```sh
saturn run <you>/project/main.jl
saturn run <you>/project/analysis.R
saturn run <you>/project/analysis.py
saturn run --size large <you>/project/main.jl
saturn run -e START=2014-01 <you>/project/main.jl
```

The fourth line asks for a bigger machine (see the sizes below); the fifth sets an extra environment variable in the job.

- **Julia:** packages come from the `Project.toml` (and `Manifest.toml`) next to the script or in a folder above it.
- **R:** the image has R 4.6 with terra, sf, GDAL, the tidyverse and more. For anything else, keep an `renv.lock` next to the script; see [R packages with renv](#r-packages-with-renv).
- **Python:** packages come from the `pyproject.toml` (and `uv.lock`) next to the script or in a folder above it, a `requirements.txt` next to the script, or the script's own header; see [Python packages](#python-packages).
- Julia and R packages are cached in the workspace, so only the first run installs them.
- Anything the job writes under `/workspace/<you>/...` appears in `~/aws/workspace/<you>/...` within a minute or two.

| `--size` | Machine | Approx. cost per hour |
| --- | --- | --- |
| `small` | 1 core, 4 GB | $0.06 |
| `medium` (default) | 4 cores, 16 GB | $0.25 |
| `large` | 8 cores, 60 GB | $0.62 |
| `xlarge` | 16 cores, 120 GB | $1.24 |
| `max` | 32 cores, 244 GB | $2.50 |

For a machine in between, `CPU=` and `MEMORY=` take exact Fargate units instead (e.g. `CPU=8192 MEMORY=32768`).

You pay per second, only while a job runs. `SPOT=1` is about 70% cheaper but AWS may interrupt the job.

The [examples](examples/) folder has a first script in each language; copy it into your workspace folder to try it.

## R packages with renv

[renv](https://rstudio.github.io/renv/) records which R packages (and versions) a project uses in a file called `renv.lock`. When a job's folder has one, the job installs exactly those packages before running your script, so it uses what you used on your laptop.

If you know Julia: `renv.lock` is R's `Manifest.toml`. There's no `Project.toml`; renv works out which packages you use by reading your code (`library(x)`, `require(x)`, `x::f()`).

You don't need renv if your script only uses packages already in the image (terra, sf, the tidyverse, ...). Add it when you need something else, or exact versions.

**Start a project** (once, in R, with the project folder as the working directory):

```r
setwd("~/aws/workspace/<you>/project")
renv::init()
```

This creates `renv.lock`, a `renv/` folder and an `.Rprofile`. From now on, R started in this folder uses the project's own packages. The packages themselves are installed on your laptop (`RENV_PATHS_LIBRARY_ROOT`, set by `saturn connect`), not in the shared workspace; in RStudio, run `readRenviron("~/.config/saturn-v/env")` before `renv::init()`.

**Add a package:**

```r
install.packages("fixest")
renv::snapshot()
```

`install.packages()` only installs it on your laptop. **`renv::snapshot()` writes it to `renv.lock`**, which is what the job reads. If you forget it, the job won't have the package.

**Check what's recorded**, and whether your code uses packages that aren't:

```r
renv::status()
```

**A package from GitHub instead of CRAN:**

```r
renv::install("rstudio/gt")
renv::snapshot()
```

**R versions.** A job runs on the same R version (major.minor) your `renv.lock` was made with, so packages that work on your laptop work in the job. R 4.5 and 4.6 are available; without a `renv.lock`, jobs use the newest. To choose one yourself: `R_VERSION=4.6 saturn run ...`. If your laptop has an R version AWS doesn't offer, `saturn run` says so; upgrade R on your laptop, then run `renv::update()` and `renv::snapshot()` in the project.

**Then run the job as usual:**

```sh
saturn run <you>/project/analysis.R
```

The first run installs the packages (pre-built for Linux, so usually a minute or two) and caches them in the workspace; later runs start straight away.

**Is a package already in the image?** One way to check:

```r
cat(requireNamespace("fixest", quietly = TRUE), "\n")
```

saved as `check.R` and run with `saturn run <you>/check.R` (about a cent).

## Python packages

Python jobs run with [uv](https://docs.astral.sh/uv/), which installs the packages your script needs before running it, and the Python version it asks for (3.13 otherwise). Say what the script needs in one of these ways:

- **A `pyproject.toml`** in the script's folder or a folder above it (a uv project: `uv init`, `uv add pandas`). The `uv.lock` next to it pins exact versions; keep both.
- **A `requirements.txt`** next to the script.
- **A header in the script itself**, for a single file (`uv add --script analysis.py pandas` writes it):

  ```python
  # /// script
  # dependencies = ["pandas", "pymysql"]
  # ///
  ```

On your laptop, `uv run analysis.py` runs it the same way (install uv with `brew install uv`, or `curl -LsSf https://astral.sh/uv/install.sh | sh` on Linux). Each job installs its packages afresh, which takes a few seconds. rasterio, geopandas and pyogrio come with their own GDAL, so nothing else is needed for the satellite data.

## Watching and managing jobs

```sh
saturn list
saturn logs <task-id>
saturn stop <task-id>
saturn costs
```

`saturn list` shows running jobs. `saturn logs` follows a job's output again; Ctrl-C stops following, not the job. `saturn costs` shows spending per month and person.

When any job ends, its cost is recorded and a summary (who, script, machine, runtime, cost) is posted to the team's Zulip channel, tagging the person who ran it. This happens in AWS, so it works even if your laptop is closed. Every Monday at 09:00 a summary of the past week follows, with jobs and spend per person, and how much each workspace folder stores, when it last changed and its last job.

## The database

The data is in `mayin-data`, a MariaDB database on Amazon RDS:

| Database | What |
| --- | --- |
| `hhd` | CPHS household survey: `hh_income_monthly`, `member_income_monthly`, `hh_consumption_monthly`, `hh_consumption_weekly`, `people_of_india_wave`, `aspirational_wave`, `hh_char` |
| `fld` | Prowess (`prowess_sa_*`, `prowess_ca_*`), CapEx (`capex_*`), `identity`, `ic_nse_daily`: 127 tables |
| `maps` | Boundaries: `states`, `districts_2011`, `districts_states_hr`, `homogeneous_regions` |
| `tmp` | Scratch tables |

Jobs and laptops share one login (`aws_jobs`), which can **only read**: queries and views, no inserts, updates, deletes or table changes. Save results as files in the workspace instead. The password reaches your code through `DB_PASSWORD`, so it never appears in a script.

**Install the package once:**

| | On your laptop (once) | In a job |
| --- | --- | --- |
| R | `install.packages("RMariaDB")` (and `dbplyr` for dplyr) | Already in the R image. With an `renv.lock`, keep `library(RMariaDB)` at the top of the script and run `renv::snapshot()` |
| Julia | `]add MySQL DBInterface DataFrames`, in the project (`julia --project=.`) | Comes from the project's `Project.toml` |
| Python | `uv add --script analysis.py pymysql pandas` (or add them to `pyproject.toml` / `requirements.txt`) | Comes from the same list |

Then connect with saturn's helper. The same lines work on your laptop (through the tunnel) and in a job:

R:

```r
source(Sys.getenv("SATURN_R"))
con <- saturn_db("fld")

df <- DBI::dbGetQuery(con, "SELECT co_code, company_name FROM identity WHERE co_code IN (11, 12)")

library(dplyr)    # or with dplyr: it writes the SQL, and only the result comes back
x <- tbl(con, "identity") |> filter(co_code == 11) |> select(co_code, company_name) |> collect()

DBI::dbDisconnect(con)
```

Julia:

```julia
using DataFrames
include(ENV["SATURN_JL"])
con = saturn_db("fld")

df = DBInterface.execute(con, "SELECT co_code, company_name FROM identity WHERE co_code IN (11, 12)") |> DataFrame
DBInterface.close!(con)
```

Python:

```python
import os
exec(open(os.environ["SATURN_PY"]).read())
con = saturn_db("fld")

df = con.sql("SELECT co_code, company_name FROM identity WHERE co_code IN (11, 12)").df()   # .df() pandas; .pl() polars
con.close()
```

`DBI::dbListTables(con)` (R), `saturn_tables(con)` (Julia) and `con.tables()` (Python) list the tables. A query can use another database's tables by name: connected to `hhd`, `SELECT company_name FROM fld.identity WHERE co_code = 11` works.

The SQL is MariaDB's (MySQL's). Column names are as stored, mostly in capitals (`STATE`, `YEAR`, `TOT_INC`), and come back that way: `df$TOT_INC`, not `df$tot_inc`. In SQL itself, case doesn't matter.

**Large CPHS tables are slow to filter for now.** Most `hhd` tables have no indexes yet, so a query reads the whole table even when it asks for one state: counting Karnataka's rows in `hh_income_monthly` (12 GB) takes about 4 minutes, and `member_income_monthly` is 59 GB. Until that's improved:

- **Run big `hhd` queries as a job**, not on your laptop, and save the result to the workspace for reuse.
- **Explore with `LIMIT`** (`SELECT * FROM hh_income_monthly LIMIT 100`) before running a full query.
- **Do the heavy work in SQL** (joins, aggregations), so only the answer travels.

`fld` and `maps` have indexes and answer quickly.

**Maps.** The `maps` tables keep each boundary in a `SHAPE` geometry column (latitude and longitude, WGS 84). MariaDB's spatial functions work on it directly. Which state is Bengaluru in?

```sql
SELECT st_nm FROM states WHERE ST_Contains(SHAPE, POINT(77.59, 12.97))   -- Karnataka
```

To get the shapes into R, with sf (`install.packages("sf")`):

```r
source(Sys.getenv("SATURN_R"))
maps <- saturn_db("maps")

s <- DBI::dbGetQuery(maps, "SELECT st_nm, ST_AsBinary(SHAPE) AS wkb FROM states")
s <- sf::st_as_sf(data.frame(st_nm = s$st_nm), geometry = sf::st_as_sfc(structure(s$wkb, class = "WKB"), crs = 4326))
plot(sf::st_geometry(s))
```

In Python, `shapely.from_wkb()` reads the same `ST_AsBinary(SHAPE)` column.

**Other clients and troubleshooting**

- The database only accepts encrypted connections; `saturn_db()` asks for that. Connecting yourself, use `client.flag = RMariaDB::CLIENT_SSL` in R or `ssl_mode=:required` in Julia; without it, R on a Mac fails with "Connections using insecure transport are prohibited".
- Other clients work through the tunnel too: host `127.0.0.1`, port `3306`, and the user name and password from `~/.config/saturn-v/env`. For example `mysql -h 127.0.0.1 -P 3306 -u aws_jobs -p` (`brew install mysql-client`), or DBeaver, TablePlus or Sequel Ace.
- If `echo $DB_HOST` prints nothing, this terminal doesn't have the variables: it was opened before `saturn connect` (open a new one, or run `eval "$(saturn connect)"`). In RStudio, run `readRenviron("~/.config/saturn-v/env")`.
- If something on your laptop already uses port 3306 (a local MySQL, say), connect with `DB_LOCAL_PORT=13306 saturn connect`; `DB_PORT` then says 13306.
- The tunnel goes through a small server on AWS, which `saturn connect` starts if it's stopped.

## Good to know

- The workspace is shared by everyone. Keep to your own folder; if two people save the same file, the last save wins.
- Folders you create in the workspace (in Finder, Terminal, R or Julia) show up in jobs, even while empty. This needs the current `saturn`: after `git pull`, run `saturn disconnect` and `saturn connect` once. Scripts are still safest creating their own output folders, e.g. `dir.create("../DOC/IMAGES", recursive = TRUE, showWarnings = FALSE)`.

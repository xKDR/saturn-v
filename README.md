# saturn-v

Run heavy Julia, R and Python jobs on AWS from your laptop, with the same code in both places.

You work on your laptop as usual. When a job needs more than your laptop has, `saturn run` sends it to a machine on AWS Fargate (up to 32 cores and 244 GB of memory), streams its output to your terminal, and records what it cost. Your laptop can sleep or disconnect while the job runs.

`saturn connect` gives your laptop the same setup a job has: the shared workspace, the satellite data and the database, under the same environment variable names. A script you try locally runs unchanged on AWS.

The AWS side (buckets, cluster, permissions) is managed in [xKDR/AWS-Server](https://github.com/xKDR/AWS-Server).

## Getting access

Ask an admin to create your AWS user and add it to the `saturn-v-users` group. You'll get a sign-in link, a user name and a password. At your first sign-in, set a new password and add MFA under **Security credentials**.

## Setup (once)

### On a Mac

Install the tools, then clone this repository into any folder you like and run the last three lines from inside it:

```sh
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
brew install awscli rclone
brew install --cask session-manager-plugin
git clone git@github.com:xKDR/saturn-v.git
cd saturn-v
echo "export PATH=\"$PWD/bin:\$PATH\"" >> ~/.zshrc
echo '[ -f ~/.config/saturn-v/env ] && set -a && . ~/.config/saturn-v/env && set +a' >> ~/.zshrc
aws configure set region ap-south-1 --profile aws-server
```

The first `echo` puts `saturn` on your path (wherever you cloned it); the second loads the job environment in every terminal while you're connected. The last line saves the AWS region (Mumbai), so signing in doesn't ask for it. Open a new terminal afterwards.

### On Linux

The same tools, from each project's own packages. On Debian or Ubuntu (on Fedora, use `dnf` and the `.rpm` plugin noted below):

```sh
sudo apt install -y git unzip curl python3 perl fuse3
curl -fsSL "https://awscli.amazonaws.com/awscli-exe-linux-$(uname -m).zip" -o /tmp/awscliv2.zip
unzip -q /tmp/awscliv2.zip -d /tmp && sudo /tmp/aws/install
curl -fsSL https://rclone.org/install.sh | sudo bash
curl -fsSL "https://s3.amazonaws.com/session-manager-downloads/plugin/latest/ubuntu_64bit/session-manager-plugin.deb" -o /tmp/smp.deb
sudo dpkg -i /tmp/smp.deb
git clone git@github.com:xKDR/saturn-v.git
cd saturn-v
echo "export PATH=\"$PWD/bin:\$PATH\"" >> ~/.bashrc
echo '[ -f ~/.config/saturn-v/env ] && set -a && . ~/.config/saturn-v/env && set +a' >> ~/.bashrc
aws configure set region ap-south-1 --profile aws-server
```

- **Fedora/RHEL:** `sudo dnf install -y git unzip curl python3 perl fuse3`, and the plugin with `sudo dnf install -y https://s3.amazonaws.com/session-manager-downloads/plugin/latest/linux_64bit/session-manager-plugin.rpm`. On ARM machines use `ubuntu_arm64` / `linux_arm64` in the plugin URL.
- **Zsh users:** use `~/.zshrc` instead of `~/.bashrc`.
- **Machines without a browser** (a server over SSH): sign in with `aws login --profile aws-server --remote`. It prints a link to open on any device, then asks for the code that page shows.
- On Linux, the mounts use FUSE.

## Each session

```sh
aws login --profile aws-server
saturn connect
```

`aws login` opens your browser to sign in; a sign-in lasts up to 12 hours. If it asks for a region, enter `ap-south-1`. `saturn connect` then:

- mounts the shared workspace at `~/aws/workspace`, and the satellite data and the [datasets](#datasets-parquet), read-only, at `~/aws/satellite` and `~/aws/datasets`;
- opens a tunnel to the database on `127.0.0.1:3306`;
- creates **your own folder** in the workspace, `~/aws/workspace/<your user name>` (see [Organising the workspace](#organising-the-workspace));
- writes the job environment to `~/.config/saturn-v/env` (readable only by you), which new terminals load;
- keeps the tunnel healthy in the background, reopening it if it drops.

For the terminal you're in, use `eval "$(saturn connect)"` instead, which also sets the variables there. `saturn status` shows what's connected; `saturn disconnect` undoes it all.

The first `saturn connect` creates a mount key on your account (kept in `~/.config/saturn-v/`), which AWS only lets use the three buckets. The mounts use it, so they keep working after your 12-hour sign-in expires. AWS allows two keys per person, so two computers; for a third, delete an old key under Security credentials in the AWS console. For everything else (running jobs, reopening the tunnel), sign in again with `aws login --profile aws-server` when asked.

## Organising the workspace

Everyone shares one workspace, so keep to this layout:

| Folder | For |
| --- | --- |
| `<your user name>/` (e.g. `sanjuktha.athreya/`) | your own work. `saturn connect` creates it; `MY_WORKSPACE` points to it, on your laptop and in your jobs |
| `shared/<project>/` | work several people use; agree on the folder name with them |
| `db/`, `costs/`, `.julia/`, `.R/` | used by saturn itself; leave them alone |

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
| `DATASETS` | `s3://<bucket>` | `~/aws/datasets` |
| `DATA_BUCKET`, `AWS_REGION` | the satellite bucket and AWS region | the same |
| `DB_HOST`, `DB_PORT` | the database | `127.0.0.1` and the tunnel port |
| `DB_USER`, `DB_PASSWORD` | the shared `aws_jobs` login | the same |

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
CPU=16384 MEMORY=65536 saturn run <you>/project/main.jl
saturn run -e START=2014-01 <you>/project/main.jl
```

The third line asks for a bigger machine (16 cores, 64 GB); the fourth sets an extra environment variable in the job.

- **Julia:** packages come from the `Project.toml` (and `Manifest.toml`) next to the script or in a folder above it.
- **R:** the image has R 4.6 with terra, sf, GDAL, the tidyverse and more. For anything else, keep an `renv.lock` next to the script; see [R packages with renv](#r-packages-with-renv).
- **Python:** packages come from the `pyproject.toml` (and `uv.lock`) next to the script or in a folder above it, a `requirements.txt` next to the script, or the script's own header; see [Python packages](#python-packages).
- Julia and R packages are cached in the workspace, so only the first run installs them.
- Anything the job writes under `/workspace/<you>/...` appears in `~/aws/workspace/<you>/...` within a minute or two.

| Size | Settings | Approx. cost per hour |
| --- | --- | --- |
| Default | 4 cores, 16 GB | $0.25 |
| Large | `CPU=16384 MEMORY=122880` (16 cores, 120 GB) | $1.25 |
| Largest | `CPU=32768 MEMORY=249856` (32 cores, 244 GB) | $2.50 |

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

This creates `renv.lock`, a `renv/` folder and an `.Rprofile`. From now on, R started in this folder uses the project's own packages.

**Add a package:**

```r
install.packages("RMariaDB")
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
cat(requireNamespace("RMariaDB", quietly = TRUE), "\n")
```

saved as `check.R` and run with `saturn run <you>/check.R` (about a cent).

## Python packages

Python jobs run with [uv](https://docs.astral.sh/uv/), which installs the packages your script needs before running it, and the Python version it asks for (3.13 otherwise). Say what the script needs in one of these ways:

- **A `pyproject.toml`** in the script's folder or a folder above it (a uv project: `uv init`, `uv add pandas`). The `uv.lock` next to it pins exact versions; keep both.
- **A `requirements.txt`** next to the script.
- **A header in the script itself**, for a single file (`uv add --script analysis.py pandas` writes it):

  ```python
  # /// script
  # dependencies = ["pandas", "duckdb"]
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

When any job ends, its cost is recorded and a summary (who, script, machine, runtime, cost) is posted to the team's Mattermost channel. This happens in AWS, so it works even if your laptop is closed.

## Datasets (Parquet)

Large datasets are kept as Parquet files in a read-only datasets bucket, laid out for fast filtering. Querying them with DuckDB is much faster than the database: a job pulled all-India CPHS income, consumption and member income in under 2 minutes, where the same queries against MariaDB take hours.

| Dataset | What | Partitioned by | Sorted by |
| --- | --- | --- | --- |
| `hhd` | CPHS household survey: `hh_consumption_monthly`, `hh_consumption_weekly`, `hh_income_monthly`, `member_income_monthly`, `people_of_india_wave`, `aspirational_wave`, `hh_char` | `YEAR`, `STATE` | `MONTH_SLOT_DATE`, `DISTRICT` |

`DATASETS` is `s3://<bucket>` in a job and `~/aws/datasets` (a read-only mount) on your laptop, so the same code works in both. The tables have the same names and columns as in the database; `MONTH_SLOT_DATE` is a date and `YEAR` a whole number.

Connect with saturn's helper, then query the tables by name, with SQL or dplyr. Do this once at the top of a script; the same lines work on your laptop and in a job.

R (needs the `duckdb` package):

```r
source(Sys.getenv("SATURN_R"))
con <- saturn_db("hhd")

df <- DBI::dbGetQuery(con, "SELECT YEAR, AVG(TOT_INC) AS avg_income
                            FROM hh_income_monthly
                            WHERE STATE = 'Karnataka' AND YEAR >= 2022 AND RESPONSE_STATUS = 'Accepted'
                            GROUP BY YEAR ORDER BY YEAR")
```

Julia (add `DuckDB`, `DBInterface` and `DataFrames` to the `Project.toml`):

```julia
include(ENV["SATURN_JL"])
con = saturn_db("hhd")

df = DBInterface.execute(con, "SELECT * FROM member_income_monthly WHERE STATE = 'Karnataka' AND YEAR = 2023 LIMIT 10") |> DataFrame
```

Python (add `duckdb` to the script's packages):

```python
import os
exec(open(os.environ["SATURN_PY"]).read())
con = saturn_db("hhd")

df = con.sql("SELECT * FROM member_income_monthly WHERE STATE = 'Karnataka' AND YEAR = 2023 LIMIT 10").df()   # .df() needs pandas; .pl() polars
```

`saturn_db()` finds the dataset's tables by itself and makes each one available under its database name. To use several datasets in one query, add them to the same connection: `saturn_db("fld", con = con)`.

After that, query the tables with SQL as before, or with dplyr: `tbl(con, "hh_income_monthly") |> filter(STATE == "Karnataka")`.

- **Filter on `YEAR` and `STATE`** whenever you can: those skip whole folders. Filters on `MONTH_SLOT_DATE` and `DISTRICT` are fast too.
- **Do the heavy work in SQL** (joins, date conversions, sorting), not in R, Julia or Python afterwards: DuckDB uses every core. Pull only the result into R, Julia or Python.
- **On your laptop,** a query reads only the columns and folders it needs, but all-India pulls of many columns are best run as a job.
- The `hhd` data is a copy of the database as of 27 Oct 2026.

## The database

Jobs and laptops share one database login (`aws_jobs`). It can **only read** the `mayin-data` MariaDB databases (`fld`, `hhd`, `maps`, `tmp`, ...): queries and views, but no inserts, updates, deletes or table changes. Save results to files in the workspace instead. The password is kept in AWS Secrets Manager and reaches your code through `DB_PASSWORD`, so it never appears in a script.

R (add `RMariaDB` to the project's `renv.lock`):

```r
con <- DBI::dbConnect(RMariaDB::MariaDB(),
  host = Sys.getenv("DB_HOST"), port = as.integer(Sys.getenv("DB_PORT")),
  user = Sys.getenv("DB_USER"), password = Sys.getenv("DB_PASSWORD"),
  dbname = "fld", client.flag = RMariaDB::CLIENT_SSL)
df <- DBI::dbGetQuery(con, "SELECT * FROM some_table LIMIT 10")
DBI::dbDisconnect(con)
```

Julia (add `MySQL`, `DBInterface` and `DataFrames` to the `Project.toml`):

```julia
using MySQL, DBInterface, DataFrames
con = DBInterface.connect(MySQL.Connection, ENV["DB_HOST"], ENV["DB_USER"], ENV["DB_PASSWORD"];
                          port=parse(Int, ENV["DB_PORT"]), db="fld", ssl_mode=:required)
df = DBInterface.execute(con, "SELECT * FROM some_table LIMIT 10") |> DataFrame
DBInterface.close!(con)
```

The database only accepts encrypted connections. That's what `client.flag = RMariaDB::CLIENT_SSL` and `ssl_mode=:required` ask for; without them, R on a Mac fails with "Connections using insecure transport are prohibited".

Other database clients work through the tunnel too: host `127.0.0.1`, port `3306`, and the user name and password from `~/.config/saturn-v/env`. For example `mysql -h 127.0.0.1 -P 3306 -u aws_jobs -p` (`brew install mysql-client`), or a GUI such as DBeaver, TablePlus or Sequel Ace.

If `echo $DB_HOST` prints nothing, this terminal doesn't have the variables: it was opened before `saturn connect` (open a new one, or run `eval "$(saturn connect)"`), or the setup line is missing from `~/.zshrc`. In RStudio, run `readRenviron("~/.config/saturn-v/env")`.

If something on your laptop already uses port 3306 (a local MySQL, say), connect with `DB_LOCAL_PORT=13306 saturn connect`; `DB_PORT` then says 13306.

## Good to know

- The workspace is shared by everyone. Keep to your own folder; if two people save the same file, the last save wins.
- Folders you create in the workspace (in Finder, Terminal, R or Julia) show up in jobs, even while empty. This needs the current `saturn`: after `git pull`, run `saturn disconnect` and `saturn connect` once. Scripts are still safest creating their own output folders, e.g. `dir.create("../DOC/IMAGES", recursive = TRUE, showWarnings = FALSE)`.
- The database tunnel goes through a small server on AWS, which `saturn connect` starts if it's stopped.

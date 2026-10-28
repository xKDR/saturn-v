# saturn-v

Run heavy Julia and R jobs on AWS from your laptop, with the same code in both places.

You work on your laptop as usual. When a job needs more than your laptop has, `saturn run` sends it to a machine on AWS Fargate (up to 32 cores and 244 GB of memory), streams its output to your terminal, and records what it cost. Your laptop can sleep or disconnect while the job runs.

`saturn connect` gives your laptop the same setup a job has: the shared workspace, the satellite data and the database, under the same environment variable names. A script you try locally runs unchanged on AWS.

The AWS side (buckets, cluster, permissions) is managed in [xKDR/AWS-Server](https://github.com/xKDR/AWS-Server).

## Getting access

Ask an admin to create your AWS user and add it to the `saturn-v-users` group. You'll get a sign-in link, a user name and a password. At your first sign-in, set a new password and add MFA under **Security credentials**.

## Setup (once, on a Mac)

```sh
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
brew install awscli rclone
brew install --cask session-manager-plugin
git clone git@github.com:xKDR/saturn-v.git ~/REPOSITORIES/saturn-v
```

Then add these two lines to `~/.zshrc` and open a new terminal. The first puts `saturn` on your path; the second loads the job environment in every terminal while you're connected.

```sh
export PATH="$HOME/REPOSITORIES/saturn-v/bin:$PATH"
[ -f ~/.config/saturn-v/env ] && set -a && . ~/.config/saturn-v/env && set +a
```

## Each session

```sh
aws login --profile aws-server
saturn connect
```

`aws login` opens your browser to sign in (region `ap-south-1`); a sign-in lasts up to 12 hours. `saturn connect` then:

- mounts the shared workspace at `~/aws/workspace` and the satellite data, read-only, at `~/aws/satellite`;
- opens a tunnel to the database on `127.0.0.1:3306`;
- writes the job environment to `~/.config/saturn-v/env` (readable only by you), which new terminals load.

For the terminal you're in, use `eval "$(saturn connect)"` instead, which also sets the variables there. `saturn status` shows what's connected; `saturn disconnect` undoes it all.

If your sign-in expires, run `aws login --profile aws-server` again. The mounted folders pause meanwhile and carry on afterwards.

## The same code on your laptop and on AWS

These variables are set in every job, and on your laptop after `saturn connect`:

| Variable | In a job | On your laptop |
| --- | --- | --- |
| `WORKSPACE` | `/workspace` | `~/aws/workspace` |
| `SATELLITE` | `/vsis3/<bucket>` (read straight from S3) | `~/aws/satellite` |
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

Paths relative to your script's folder also work in both places.

Reading satellite files on your laptop downloads them (about $0.11/GB), so try things on a few files or a small area there, and do the heavy reading in a job, where it's free.

RStudio and other apps started from the Dock don't read `~/.zshrc`. In R, load the environment with `readRenviron("~/.config/saturn-v/env")`.

## Running a job

Put your script in your own folder in the workspace, for example `~/aws/workspace/<you>/project/`, then:

```sh
saturn run <you>/project/main.jl
saturn run <you>/project/analysis.R
CPU=16384 MEMORY=65536 saturn run <you>/project/main.jl
saturn run -e START=2014-01 <you>/project/main.jl
```

The third line asks for a bigger machine (16 cores, 64 GB); the fourth sets an extra environment variable in the job.

- **Julia:** packages come from the `Project.toml` (and `Manifest.toml`) next to the script or in a folder above it.
- **R:** the image has R 4.6 with terra, sf, GDAL and the tidyverse. For anything else, keep an `renv.lock` next to the script (`renv::snapshot()` makes one).
- Packages are cached in the workspace, so only the first run installs them.
- Anything the job writes under `/workspace/<you>/...` appears in `~/aws/workspace/<you>/...` within a minute or two.

| Size | Settings | Approx. cost per hour |
| --- | --- | --- |
| Default | 4 cores, 16 GB | $0.25 |
| Large | `CPU=16384 MEMORY=122880` (16 cores, 120 GB) | $1.25 |
| Largest | `CPU=32768 MEMORY=249856` (32 cores, 244 GB) | $2.50 |

You pay per second, only while a job runs. `SPOT=1` is about 70% cheaper but AWS may interrupt the job.

The [examples](examples/) folder has a first script in each language; copy it into your workspace folder to try it.

## Watching and managing jobs

```sh
saturn list
saturn logs <task-id>
saturn stop <task-id>
saturn costs
```

`saturn list` shows running jobs. `saturn logs` follows a job's output again; Ctrl-C stops following, not the job. `saturn costs` shows spending per month and person.

## The database

Jobs and laptops share one database login (`aws_jobs`). It can read and write the `mayin-data` MariaDB databases (`fld`, `hhd`, `maps`, `tmp`, ...) but has no admin rights. The password is kept in AWS Secrets Manager and reaches your code through `DB_PASSWORD`, so it never appears in a script.

R (add `RMariaDB` to the project's `renv.lock`):

```r
con <- DBI::dbConnect(RMariaDB::MariaDB(),
  host = Sys.getenv("DB_HOST"), port = as.integer(Sys.getenv("DB_PORT")),
  user = Sys.getenv("DB_USER"), password = Sys.getenv("DB_PASSWORD"),
  dbname = "fld")
df <- DBI::dbGetQuery(con, "SELECT * FROM some_table LIMIT 10")
DBI::dbDisconnect(con)
```

Julia (add `MySQL`, `DBInterface` and `DataFrames` to the `Project.toml`):

```julia
using MySQL, DBInterface, DataFrames
con = DBInterface.connect(MySQL.Connection, ENV["DB_HOST"], ENV["DB_USER"], ENV["DB_PASSWORD"];
                          port=parse(Int, ENV["DB_PORT"]), db="fld")
df = DBInterface.execute(con, "SELECT * FROM some_table LIMIT 10") |> DataFrame
DBInterface.close!(con)
```

Other database clients work through the tunnel too: host `127.0.0.1`, port `3306`, and the user name and password from `~/.config/saturn-v/env`. For example `mysql -h 127.0.0.1 -P 3306 -u aws_jobs -p` (`brew install mysql-client`), or a GUI such as DBeaver, TablePlus or Sequel Ace.

If something on your laptop already uses port 3306 (a local MySQL, say), connect with `DB_LOCAL_PORT=13306 saturn connect`; `DB_PORT` then says 13306.

## Good to know

- The workspace is shared by everyone. Keep to your own folder; if two people save the same file, the last save wins.
- `saturn connect` is macOS-only for now.
- The database tunnel goes through a small server on AWS, which `saturn connect` starts if it's stopped.

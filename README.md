# saturn-v

Run heavy Julia and R jobs on AWS from your laptop.

You work on your laptop as usual. When a job needs more than your laptop has, `saturn run` sends it to a machine on AWS Fargate (up to 32 cores and 244 GB of memory), streams its output to your terminal, and records what it cost. Your laptop can sleep or disconnect while the job runs.

Everyone shares one workspace: a folder that is `~/aws/workspace` on your laptop and `/workspace` inside every job. The satellite data is mounted read-only at `~/aws/satellite`.

The AWS side (buckets, cluster, permissions) is managed in [xKDR/AWS-Server](https://github.com/xKDR/AWS-Server).

## Getting access

Ask an admin to create your AWS user and add you to the team. You'll get a sign-in link, a user name and a password.

## Setup (once, on a Mac)

```sh
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
brew install awscli rclone
git clone git@github.com:xKDR/saturn-v.git ~/REPOSITORIES/saturn-v
echo 'export PATH="$HOME/REPOSITORIES/saturn-v/bin:$PATH"' >> ~/.zprofile    # then open a new terminal
```

## Each session

```sh
aws login --profile aws-server     # browser sign-in; region ap-south-1; lasts up to 12 hours
saturn mount                       # ~/aws/workspace and ~/aws/satellite
```

If your sign-in expires, run `aws login --profile aws-server` again. Mounted folders pause meanwhile and carry on afterwards.

## Running a job

Put your script in your own folder in the workspace, for example `~/aws/workspace/<you>/project/`, then:

```sh
saturn run <you>/project/main.jl                       # Julia
saturn run <you>/project/analysis.R                    # R
CPU=16384 MEMORY=65536 saturn run <you>/project/main.jl  # bigger machine: 16 cores, 64 GB
saturn run -e START=2014-01 <you>/project/main.jl      # pass settings as environment variables
```

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

## Watching and managing jobs

```sh
saturn list               # running jobs
saturn logs <task-id>     # follow a job's output again (Ctrl-C stops following, not the job)
saturn stop <task-id>
saturn costs              # spending per month and person
```

## Inside a job

| Variable | Meaning |
| --- | --- |
| `WORKSPACE` | `/workspace`, the shared folder |
| `DATA_BUCKET` | the satellite data bucket; read files as `/vsis3/$DATA_BUCKET/...` with GDAL, terra or Rasters |
| `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD` | the `mayin-data` MariaDB database (shared `aws_jobs` user) |

## The database

Jobs and laptops share one database login (`aws_jobs`), kept in AWS Secrets Manager. It can read and write the `mayin-data` MariaDB databases (`fld`, `hhd`, `maps`, `tmp`, ...) but has no admin rights. Nobody needs the password in their code.

### From a job

The connection details are in the job's environment: `DB_HOST`, `DB_PORT`, `DB_USER` and `DB_PASSWORD`.

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

### From your laptop

The database isn't reachable from the internet, so open a tunnel first and leave it running in its own terminal:

```sh
saturn db
```

It connects `127.0.0.1:3306` on your laptop to the database (starting the small tunnel server on AWS if it's stopped) and prints the command that shows the login:

```sh
aws secretsmanager get-secret-value --secret-id <arn printed by saturn db> --query SecretString --output text
```

Then connect to host `127.0.0.1`, port `3306` with that user name and password, from any client:

- Terminal: `brew install mysql-client`, then `mysql -h 127.0.0.1 -P 3306 -u aws_jobs -p`
- A GUI such as DBeaver, TablePlus or Sequel Ace
- R or Julia on your laptop: the code above with `host = "127.0.0.1"` and the login from Secrets Manager

Press Ctrl-C in the tunnel's terminal when you're done.

## Good to know

- The workspace is shared by everyone. Keep to your own folder; if two people save the same file, the last save wins.
- Reading satellite files on your laptop downloads them (about $0.11/GB). Reading them inside a job is free, so do heavy reading there.
- Mounting is macOS-only for now.

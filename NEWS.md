# What's new in saturn

`saturn update` prints the entries added since your last update. Newest first; each entry
starts with a line `## YYYY-MM-DD: headline`, followed by a few short lines for users.

## 2026-11-25: data now comes from the database (MariaDB)
- `saturn_db("hhd")` (also `fld`, `maps`) now connects to the mayin-data database, on your laptop (after `saturn connect`) and in jobs.
- Install the client once: R `install.packages("RMariaDB")`; Julia `]add MySQL DBInterface DataFrames`; Python add `pymysql` and `pandas` to your script.
- Queries are MariaDB SQL; column names are uppercase (`STATE`, `TOT_INC`).
- `~/aws/datasets` and Athena are gone: read data through `saturn_db()` instead of files.
- Big hhd queries are slow for now (minutes): run them as jobs.
- Fixed: `saturn connect` sometimes stopped before printing its exports.
- See "The database" in the guide: https://xkdr.github.io/saturn-v/#database

# saturn helpers for Julia. Load with: include(ENV["SATURN_JL"])
#
#   con = saturn_db("hhd")    # the hhd database in mayin-data (MariaDB)
#   df = DBInterface.execute(con, "SELECT YEAR, AVG(TOT_INC) FROM hh_income_monthly WHERE STATE = 'Karnataka' GROUP BY YEAR") |> DataFrame
#   saturn_tables(con)
#   DBInterface.close!(con)
#
# Works the same on a laptop (through `saturn connect`'s tunnel) and in a job: both have
# DB_HOST, DB_PORT, DB_USER and DB_PASSWORD. Needs MySQL, DBInterface and DataFrames in the
# project's Project.toml.
using MySQL, DBInterface

function saturn_db(dataset::AbstractString="hhd")
    host = get(ENV, "DB_HOST", "")
    isempty(host) && error("DB_HOST is not set: on a laptop run `saturn connect`; in a job it is set for you")
    DBInterface.connect(MySQL.Connection, host, ENV["DB_USER"], ENV["DB_PASSWORD"];
                        port=parse(Int, get(ENV, "DB_PORT", "3306")), db=dataset,
                        ssl_mode=:required) # the database accepts encrypted connections only
end

saturn_tables(con) = [String(first(r)) for r in DBInterface.execute(con, "SHOW TABLES")]

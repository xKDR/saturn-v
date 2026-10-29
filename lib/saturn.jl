# saturn helpers for Julia. Load with: include(ENV["SATURN_JL"])
#
#   con = saturn_db("hhd")    # DuckDB with every hhd table as a view
#   DBInterface.execute(con, "SELECT ... FROM hh_income_monthly WHERE STATE = 'Karnataka'") |> DataFrame
#
# Works the same on a laptop (after `saturn connect`, DATASETS is a local folder) and in
# a job (DATASETS is s3://...). Needs DuckDB and DBInterface in the project.
using DuckDB, DBInterface

function saturn_db(dataset::AbstractString; con=DBInterface.connect(DuckDB.DB))
    datasets = get(ENV, "DATASETS", "")
    isempty(datasets) && error("DATASETS is not set: run `saturn connect` first (on a laptop), or run the script with `saturn run`")
    root = rstrip(datasets, '/') * "/" * dataset
    q(sql) = DBInterface.execute(con, sql)

    if startswith(root, "s3://")
        q("INSTALL httpfs")
        q("LOAD httpfs")
        q("CREATE OR REPLACE SECRET saturn (TYPE s3, KEY_ID '$(ENV["AWS_ACCESS_KEY_ID"])', " *
          "SECRET '$(ENV["AWS_SECRET_ACCESS_KEY"])', SESSION_TOKEN '$(ENV["AWS_SESSION_TOKEN"])', REGION '$(ENV["AWS_REGION"])')")
        files = [String(r.file) for r in q("SELECT file FROM glob('$root/*/**/*.parquet')")]
        tables = unique(first.(split.(chopprefix.(files, root * "/"), "/")))
    else
        isdir(root) || error("no dataset '$dataset' in $datasets")
        tables = filter(d -> isdir(joinpath(root, d)), readdir(root))
    end
    tables = sort(filter(t -> !startswith(t, "_") && !startswith(t, "."), String.(tables)))
    isempty(tables) && error("no tables found in $root")

    for t in tables
        q("CREATE OR REPLACE VIEW \"$t\" AS SELECT * FROM read_parquet('$root/$t/**/*.parquet', hive_partitioning = true)")
    end
    @info "$dataset: $(length(tables)) tables ($(join(tables, ", ")))"
    return con
end

# saturn helpers for Julia. Load with: include(ENV["SATURN_JL"])
#
#   con = saturn_db("hhd")
#   df = DBInterface.execute(con, "SELECT year, AVG(tot_inc) AS avg_income FROM hh_income_monthly WHERE state = 'Karnataka' GROUP BY year") |> DataFrame
#   saturn_tables(con)
#
# The datasets are queried through Athena, never read directly: each query runs in AWS under
# your name and is recorded, and only its result comes back. Works the same on a laptop
# (signed in with `saturn login`) and in a job (with the job's own keys). Needs AWS, Parquet2,
# CSV, DataFrames and DBInterface in the project. The SQL is Athena's (Trino); see the guide.
using AWS, CSV, DataFrames, DBInterface, Dates, Parquet2
@service Athena
@service Glue
@service S3
@service STS

struct SaturnAthena <: DBInterface.Connection
    dataset::String
    workgroup::String
    bucket::String
    userid::String
end

# AWS keys. A job has its own; on a laptop they come from the saturn sign-in through the AWS
# CLI (AWS.jl can't read the sign-in itself), fetched again before they expire.
function saturn_aws_keys()
    lines = readlines(`aws configure export-credentials --profile $(get(ENV, "SATURN_AWS_PROFILE", "saturn-v")) --format env-no-export`)
    k = Dict(split(l, "="; limit=2) for l in lines if occursin("=", l))
    expires = haskey(k, "AWS_CREDENTIAL_EXPIRATION") ? DateTime(k["AWS_CREDENTIAL_EXPIRATION"][1:19]) : now(UTC) + Hour(1)
    AWSCredentials(k["AWS_ACCESS_KEY_ID"], k["AWS_SECRET_ACCESS_KEY"], k["AWS_SESSION_TOKEN"]; expiry=expires, renew=saturn_aws_keys)
end

function saturn_db(dataset::AbstractString="hhd")
    workgroup = get(ENV, "SATURN_ATHENA_WORKGROUP", "")
    isempty(workgroup) && error("SATURN_ATHENA_WORKGROUP is not set: on a laptop run `saturn update`, then `saturn connect`")
    region = get(ENV, "AWS_REGION", "ap-south-1")
    in_job = haskey(ENV, "AWS_ACCESS_KEY_ID") || haskey(ENV, "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI")
    global_aws_config(in_job ? AWSConfig(; region=region) : AWSConfig(; creds=saturn_aws_keys(), region=region))
    # Query results go to a folder only you can read: unload/<your AWS user id>/.
    userid = STS.get_caller_identity()["GetCallerIdentityResult"]["UserId"]
    con = SaturnAthena(dataset, workgroup, ENV["SATURN_ATHENA_RESULTS"], userid)
    tables = saturn_tables(con)
    isempty(tables) && error("no tables in '$dataset' that you may query")
    @info "$dataset: $(length(tables)) tables ($(length(tables) <= 10 ? join(tables, ", ") : "saturn_tables(con) lists them"))"
    return con
end

# Glue lists at most 100 tables at a time (fld has more).
function saturn_tables(con::SaturnAthena)
    names, params = String[], Dict{String,Any}()
    while true
        page = Glue.get_tables(con.dataset, params)
        listed = get(page, "TableList", [])
        append!(names, [t["Name"] for t in (listed isa AbstractVector ? listed : [listed])])
        token = get(page, "NextToken", "")
        isempty(token) && break
        params["NextToken"] = token
    end
    return sort(names)
end

# Runs one query in Athena and returns its result as a DataFrame. A SELECT comes back as
# Parquet (UNLOAD: typed, and quick even for large results); one that sorts (ORDER BY), or
# isn't a SELECT, as Athena's CSV, since UNLOAD may split the rows over files.
function DBInterface.execute(con::SaturnAthena, sql::AbstractString; kw...)
    sql = rstrip(strip(sql), ';')
    unload = occursin(r"^(select|with)\b"i, sql) && !occursin(r"\border\s+by\b"i, sql)
    prefix = "unload/$(con.userid)/$(Dates.format(now(UTC), "yyyymmddTHHMMSSsss"))-$(getpid())/"
    query = unload ? "UNLOAD ($sql) TO 's3://$(con.bucket)/$prefix' WITH (format = 'PARQUET', compression = 'ZSTD')" : sql
    id = Athena.start_query_execution(query, Dict("WorkGroup" => con.workgroup, "QueryExecutionContext" => Dict("Database" => con.dataset)))["QueryExecutionId"]
    q = Dict()
    while true
        q = Athena.get_query_execution(id)["QueryExecution"]
        q["Status"]["State"] in ("SUCCEEDED", "FAILED", "CANCELLED") && break
        sleep(0.3)
    end
    q["Status"]["State"] == "SUCCEEDED" || error("query $(lowercase(q["Status"]["State"])): $(get(q["Status"], "StateChangeReason", ""))")
    if unload
        listed = get(S3.list_objects_v2(con.bucket, Dict("prefix" => prefix)), "Contents", [])
        keys = [o["Key"] for o in (listed isa AbstractVector ? listed : [listed])]
        isempty(keys) && return DataFrame()
        return reduce(vcat, [DataFrame(Parquet2.Dataset(S3.get_object(con.bucket, k)); copycols=false) for k in keys]; cols=:union)
    end
    m = match(r"^s3://([^/]+)/(.+)$", q["ResultConfiguration"]["OutputLocation"])
    return CSV.read(IOBuffer(S3.get_object(m[1], m[2])), DataFrame)
end

DBInterface.close!(::SaturnAthena) = nothing
Base.show(io::IO, con::SaturnAthena) = print(io, "<saturn: Athena, dataset ", con.dataset, ">")

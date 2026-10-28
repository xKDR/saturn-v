# A first job. The same file runs on your laptop (after `saturn connect`) and on AWS:
#
#   julia --project=. main.jl                        # locally, from this folder
#   saturn run saturn-v-examples/julia/main.jl       # on AWS (path relative to the workspace)
#
# Both places have the same environment variables: WORKSPACE, SATELLITE, DATA_BUCKET,
# AWS_REGION and DB_HOST, DB_PORT, DB_USER, DB_PASSWORD.
using Dates

println("Julia $(VERSION) on $(Sys.CPU_THREADS) CPUs with $(round(Sys.total_memory() / 2^30; digits=1)) GiB, $(Threads.nthreads()) threads")
println("Workspace:      ", ENV["WORKSPACE"])
println("Satellite data: ", ENV["SATELLITE"])   # a local folder, or /vsis3/... inside a job
println("Database:       ", ENV["DB_USER"], "@", ENV["DB_HOST"], ":", ENV["DB_PORT"])

# A satellite file, for GDAL-based packages (Rasters, ArchGDAL); the same path works in both places:
satellite_file = joinpath(ENV["SATELLITE"], "ntl", "sl", "rad",
                          "SVDNB_j01_20220801-20220831_global_vcmslcfg_v10_c202209231200.avg_rade9h.tif")
println("Example file:   ", satellite_file)

out = joinpath(@__DIR__, "hello.txt")
write(out, "Hello at $(now(UTC)) UTC from $(gethostname())\n")
println("Wrote $out")

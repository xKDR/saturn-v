# A first job: shows the machine it ran on and writes a file back to the workspace.
#
#   saturn run examples/julia/main.jl          (after copying examples/ into ~/aws/workspace)
using Dates

println("Julia $(VERSION) on $(Sys.CPU_THREADS) CPUs with $(round(Sys.total_memory() / 2^30; digits=1)) GiB")
println("Threads: $(Threads.nthreads())")
println("Satellite bucket: $(get(ENV, "DATA_BUCKET", "?")), database: $(get(ENV, "DB_HOST", "?"))")

out = joinpath(@__DIR__, "hello.txt")
write(out, "Hello from Fargate at $(now(UTC)) UTC\n")
println("Wrote $out; it appears next to this script on your laptop shortly.")

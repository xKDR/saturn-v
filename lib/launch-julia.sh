# Runs inside the Julia container (bash -c "$(cat launch-julia.sh)" julia script.jl args...).
# Changes to the script's folder, installs the packages in the nearest Project.toml
# (cached in /workspace/.julia), then runs the script.
cd "$WORKSPACE/$(dirname "$1")" || exit 1

# Satellite data, read straight from S3 by GDAL (Rasters, ArchGDAL). On a laptop,
# `saturn connect` sets SATELLITE to the local mount instead, so code doesn't change.
export SATELLITE="/vsis3/$DATA_BUCKET"

# saturn's helpers (saturn_db() etc.), sent along with the job by `saturn run`.
printf '%s' "$SATURN_HELPER" >/tmp/saturn.jl && export SATURN_JL=/tmp/saturn.jl
unset SATURN_HELPER
# GDAL needs these for /vsis3/; they come from the task role and last about 6 hours.
creds=$(julia --startup-file=no -e 'using Downloads
  print(String(take!(Downloads.download("http://169.254.170.2" * ENV["AWS_CONTAINER_CREDENTIALS_RELATIVE_URI"], IOBuffer()))))')
field() { printf '%s' "$creds" | sed -n "s/.*\"$1\" *: *\"\([^\"]*\)\".*/\1/p"; }
export AWS_ACCESS_KEY_ID=$(field AccessKeyId) AWS_SECRET_ACCESS_KEY=$(field SecretAccessKey) AWS_SESSION_TOKEN=$(field Token)

julia --project=@. -e 'import Pkg; isfile(Base.active_project()) && Pkg.instantiate()' || exit 1
exec julia --project=@. "$(basename "$1")" "${@:2}"

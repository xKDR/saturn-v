# Runs inside the Julia container (bash -c "$(cat launch-julia.sh)" julia script.jl args...).
# Changes to the script's folder, installs the packages in the nearest Project.toml
# (cached in /workspace/.julia), then runs the script.
cd "$WORKSPACE/$(dirname "$1")" || exit 1
julia --project=@. -e 'import Pkg; isfile(Base.active_project()) && Pkg.instantiate()' || exit 1
exec julia --project=@. "$(basename "$1")" "${@:2}"

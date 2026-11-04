# Runs inside the R container (bash -c "$(cat launch-r.sh)" r script.R args...).
# Changes to the script's folder, installs the packages in its renv.lock if it has one
# (cached in /workspace/.R), then runs the script with Rscript.
cd "$WORKSPACE/$(dirname "$1")" || exit 1

# Satellite data, read straight from S3 by GDAL (terra, sf). On a laptop, `saturn
# connect` sets SATELLITE to the local mount instead, so code doesn't change.
export SATELLITE="/vsis3/$DATA_BUCKET"

# saturn's helpers (saturn_db() etc.), sent along with the job by `saturn run`.
# (compressed, to fit in ECS's 8 KB of job settings)
printf '%s' "$SATURN_HELPER" | base64 -d | gzip -dc >/tmp/saturn.R && export SATURN_R=/tmp/saturn.R
unset SATURN_HELPER

# GDAL (terra, sf: /vsis3/ paths) reads S3 with these; R's own S3 packages use them too.
# They come from the task role and last about 6 hours.
# (The image has no curl, so R fetches them.)
creds=$(Rscript -e 'cat(readLines(paste0("http://169.254.170.2", Sys.getenv("AWS_CONTAINER_CREDENTIALS_RELATIVE_URI")), warn = FALSE))')
field() { printf '%s' "$creds" | sed -n "s/.*\"$1\" *: *\"\([^\"]*\)\".*/\1/p"; }
export AWS_ACCESS_KEY_ID=$(field AccessKeyId) AWS_SECRET_ACCESS_KEY=$(field SecretAccessKey) AWS_SESSION_TOKEN=$(field Token)

# R only uses R_LIBS_USER (set in the task definition) once the folder exists.
Rscript -e 'v <- paste(R.version$major, sub("\\..*", "", R.version$minor), sep = ".")
            dir.create(sub("%v", v, Sys.getenv("R_LIBS_USER"), fixed = TRUE), recursive = TRUE, showWarnings = FALSE)'

if [ -f renv.lock ]; then
  # Keep each project's renv library in the shared cache, not in the project folder.
  export RENV_PATHS_LIBRARY_ROOT=/workspace/.R/renv/library
  # renv installs from the repository named in renv.lock. Made on a laptop, that's usually
  # CRAN, which has only source packages for Linux: arrow or sf would then compile for
  # an hour. The image's repository is Posit Package Manager, which has ready-built Linux
  # binaries; use its latest snapshot (the image's own is pinned to the date it was built,
  # older than many lockfiles). Anything still built from source uses every core.
  # (Passed to restore() itself: a project made with renv::init() sets its repositories
  # from the lockfile when it starts, over any other setting.)
  repo=$(Rscript --no-init-file -e 'cat(getOption("repos")[["CRAN"]])') # the image's, not the project's
  case $repo in
  */__linux__/*) repos="c(CRAN = '${repo%/*}/latest')" ;;
  *) repos=NULL ;;
  esac
  export MAKEFLAGS="-j$(nproc)"
  export RENV_DOWNLOAD_METHOD=libcurl # the image has no curl program; R's libcurl is what downloads
  Rscript -e 'if (!requireNamespace("renv", quietly = TRUE)) install.packages("renv")' || exit 1
  Rscript -e "renv::restore(project = '.', prompt = FALSE, repos = $repos)" || exit 1
  R_LIBS=$(Rscript -e 'cat(renv::paths$library(project = "."))') && export R_LIBS
fi

exec Rscript "$(basename "$1")" "${@:2}"

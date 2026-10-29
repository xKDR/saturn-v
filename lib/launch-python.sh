# Runs inside the Python container (bash -c "$(cat launch-python.sh)" python script.py args...).
# Changes to the script's folder and runs the script with uv, which first installs its
# packages (in seconds, so afresh in every job): from the nearest pyproject.toml (and uv.lock),
# a requirements.txt next to the script, or the script's own `# /// script` header.
cd "$WORKSPACE/$(dirname "$1")" || exit 1

# Satellite data, read straight from S3 by GDAL (rasterio, geopandas). On a laptop,
# `saturn connect` sets SATELLITE to the local mount instead, so code doesn't change.
export SATELLITE="/vsis3/$DATA_BUCKET"

# saturn's helpers (saturn_db() etc.), sent along with the job by `saturn run`.
printf '%s' "$SATURN_HELPER" >/tmp/saturn.py && export SATURN_PY=/tmp/saturn.py
unset SATURN_HELPER

# GDAL (/vsis3/ paths), boto3 and DuckDB read S3 with these; they come from the task role
# and last about 6 hours.
eval "$(python3 -c 'import json, os, urllib.request
c = json.load(urllib.request.urlopen("http://169.254.170.2" + os.environ["AWS_CONTAINER_CREDENTIALS_RELATIVE_URI"]))
print("export AWS_ACCESS_KEY_ID=%s AWS_SECRET_ACCESS_KEY=%s AWS_SESSION_TOKEN=%s" % (c["AccessKeyId"], c["SecretAccessKey"], c["Token"]))')"

requirements=()
[ -f requirements.txt ] && requirements=(--with-requirements requirements.txt)
exec uv run "${requirements[@]}" "$(basename "$1")" "${@:2}"

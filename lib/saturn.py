# saturn helpers for Python. Load with: exec(open(os.environ["SATURN_PY"]).read())
#
#   con = saturn_db("hhd")    # DuckDB with every hhd table as a view
#   df = con.sql("SELECT ... FROM hh_income_monthly WHERE STATE = 'Karnataka'").df()
#
# Works the same on a laptop (after `saturn connect`, DATASETS is a local folder) and in
# a job (DATASETS is s3://...). Needs the duckdb package.

import os as _os


def saturn_db(dataset, con=None):
    import duckdb

    datasets = _os.environ.get("DATASETS")
    if not datasets:
        raise RuntimeError("DATASETS is not set: run `saturn connect` first (on a laptop), or run the script with `saturn run`")
    root = datasets.rstrip("/") + "/" + dataset
    if con is None:
        con = duckdb.connect()

    if root.startswith("s3://"):
        con.execute("INSTALL httpfs")
        con.execute("LOAD httpfs")
        con.execute(
            "CREATE OR REPLACE SECRET saturn (TYPE s3, KEY_ID '%s', SECRET '%s', SESSION_TOKEN '%s', REGION '%s')"
            % tuple(_os.environ.get(k, "") for k in ("AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_REGION")))
        files = [f for (f,) in con.execute("SELECT file FROM glob('%s/*/**/*.parquet')" % root).fetchall()]
        tables = {f[len(root) + 1:].split("/", 1)[0] for f in files}
    else:
        if not _os.path.isdir(root):
            raise RuntimeError("no dataset '%s' in %s" % (dataset, datasets))
        tables = {d for d in _os.listdir(root) if _os.path.isdir(_os.path.join(root, d))}
    tables = sorted(t for t in tables if not t.startswith(("_", ".")))
    if not tables:
        raise RuntimeError("no tables found in " + root)

    for t in tables:
        con.execute(
            "CREATE OR REPLACE VIEW \"%s\" AS SELECT * FROM read_parquet('%s/%s/**/*.parquet', hive_partitioning = true)"
            % (t, root, t))
    print("%s: %d tables (%s)" % (dataset, len(tables), ", ".join(tables)))
    return con

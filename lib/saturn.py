# saturn helpers for Python. Load with: exec(open(os.environ["SATURN_PY"]).read())
#
#   con = saturn_db("hhd")
#   df = con.sql("SELECT year, AVG(tot_inc) AS avg_income FROM hh_income_monthly WHERE state = 'Karnataka' GROUP BY year").df()   # pandas
#   con.sql(...).arrow()   # pyarrow;   con.sql(...).pl()   # polars;   con.tables()
#
# The datasets are queried through Athena, never read directly: each query runs in AWS under
# your name and is recorded, and only its result comes back. Works the same on a laptop
# (signed in with `saturn login`) and in a job (with the job's own keys). Needs boto3 and
# pyarrow (pandas for .df(), polars for .pl()). The SQL is Athena's (Trino); see the guide.

import os as _os


class _SaturnResult:
    def __init__(self, table):
        self._table = table

    def arrow(self):
        return self._table

    def df(self):
        return self._table.to_pandas()

    def pl(self):
        import polars
        return polars.from_arrow(self._table)

    def fetchall(self):
        return [tuple(row.values()) for row in self._table.to_pylist()]


class _SaturnAthena:
    def __init__(self, dataset):
        import boto3
        self.dataset = dataset
        self._workgroup = _os.environ.get("SATURN_ATHENA_WORKGROUP")
        if not self._workgroup:
            raise RuntimeError("SATURN_ATHENA_WORKGROUP is not set: on a laptop run `saturn update`, then `saturn connect`")
        self._bucket = _os.environ["SATURN_ATHENA_RESULTS"]
        # A job has its own keys; a laptop uses the saturn sign-in.
        in_job = _os.environ.get("AWS_ACCESS_KEY_ID") or _os.environ.get("AWS_CONTAINER_CREDENTIALS_RELATIVE_URI")
        session = boto3.Session(profile_name=None if in_job else _os.environ.get("SATURN_AWS_PROFILE", "saturn-v"),
                                region_name=_os.environ.get("AWS_REGION", "ap-south-1"))
        self._athena, self._s3, self._glue = session.client("athena"), session.client("s3"), session.client("glue")
        # Query results go to a folder only you can read: unload/<your AWS user id>/.
        self._userid = session.client("sts").get_caller_identity()["UserId"]

    def tables(self):
        pages = self._glue.get_paginator("get_tables").paginate(DatabaseName=self.dataset)
        return sorted(t["Name"] for page in pages for t in page["TableList"])

    def sql(self, query):
        """Runs one query in Athena. A SELECT comes back as Parquet (UNLOAD: typed, and quick
        even for large results); one that sorts (ORDER BY), or isn't a SELECT, as Athena's CSV,
        since UNLOAD may split the rows over files."""
        import io, re, time
        import pyarrow as pa, pyarrow.csv as pacsv, pyarrow.parquet as pq
        query = query.strip().rstrip(";")
        unload = re.match(r"(?is)\s*(select|with)\b", query) and not re.search(r"(?i)\border\s+by\b", query)
        prefix = "unload/%s/%s%06d-%d/" % (self._userid, time.strftime("%Y%m%dT%H%M%S"), time.time() % 1 * 1e6, _os.getpid())
        if unload:
            query = "UNLOAD (%s) TO 's3://%s/%s' WITH (format = 'PARQUET', compression = 'ZSTD')" % (query, self._bucket, prefix)
        qid = self._athena.start_query_execution(QueryString=query, WorkGroup=self._workgroup,
                                                 QueryExecutionContext={"Database": self.dataset})["QueryExecutionId"]
        while (q := self._athena.get_query_execution(QueryExecutionId=qid)["QueryExecution"])["Status"]["State"] in ("QUEUED", "RUNNING"):
            time.sleep(0.3)
        status = q["Status"]
        if status["State"] != "SUCCEEDED":
            raise RuntimeError("query %s: %s" % (status["State"].lower(), status.get("StateChangeReason", "")))
        read = lambda bucket, key: io.BytesIO(self._s3.get_object(Bucket=bucket, Key=key)["Body"].read())
        if unload:
            keys = [o["Key"] for o in self._s3.list_objects_v2(Bucket=self._bucket, Prefix=prefix).get("Contents", [])]
            return _SaturnResult(pa.concat_tables([pq.read_table(read(self._bucket, k)) for k in keys]) if keys else pa.table({}))
        bucket, key = q["ResultConfiguration"]["OutputLocation"][len("s3://"):].split("/", 1)
        return _SaturnResult(pacsv.read_csv(read(bucket, key)))

    def __repr__(self):
        return "<saturn: Athena, dataset %s>" % self.dataset


def saturn_db(dataset="hhd"):
    con = _SaturnAthena(dataset)
    tables = con.tables()
    if not tables:
        raise RuntimeError("no tables in '%s' that you may query" % dataset)
    print("%s: %d tables (%s)" % (dataset, len(tables), ", ".join(tables) if len(tables) <= 10 else "con.tables() lists them"))
    return con

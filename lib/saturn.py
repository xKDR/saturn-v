# saturn helpers for Python. Load with: exec(open(os.environ["SATURN_PY"]).read())
#
#   con = saturn_db("hhd")    # the hhd database in mayin-data (MariaDB)
#   df = con.sql("SELECT YEAR, AVG(TOT_INC) FROM hh_income_monthly WHERE STATE = 'Karnataka' GROUP BY YEAR").df()   # pandas
#   con.sql(...).pl()   # polars;   con.sql(...).arrow()   # pyarrow;   con.sql(...).fetchall();   con.tables()
#
# Works the same on a laptop (through `saturn connect`'s tunnel) and in a job: both have
# DB_HOST, DB_PORT, DB_USER and DB_PASSWORD. Needs pymysql (pandas for .df(), polars for
# .pl(), pyarrow for .arrow()).

import os as _os


class _SaturnResult:
    def __init__(self, columns, rows):
        self.columns, self._rows = columns, rows

    def fetchall(self):
        return self._rows

    def df(self):
        import pandas
        return pandas.DataFrame(self._rows, columns=self.columns)

    def pl(self):
        import polars
        return polars.DataFrame(self._rows, schema=self.columns, orient="row")

    def arrow(self):
        import pyarrow
        return pyarrow.Table.from_pylist([dict(zip(self.columns, r)) for r in self._rows])


class _SaturnDB:
    def __init__(self, conn):
        self.conn = conn  # the pymysql connection, for anything else

    def sql(self, query, params=None):
        with self.conn.cursor() as cur:
            cur.execute(query, params)
            columns = [c[0] for c in cur.description] if cur.description else []
            return _SaturnResult(columns, list(cur.fetchall()))

    def tables(self):
        return [t for (t,) in self.sql("SHOW TABLES").fetchall()]

    def close(self):
        self.conn.close()


def saturn_db(dataset="hhd"):
    try:
        import pymysql
    except ImportError:
        raise RuntimeError("saturn_db() needs pymysql: add it to the script's packages (uv add --script analysis.py pymysql)")
    if not _os.environ.get("DB_HOST"):
        raise RuntimeError("DB_HOST is not set: on a laptop run `saturn connect`; in a job it is set for you")
    conn = pymysql.connect(host=_os.environ["DB_HOST"], port=int(_os.environ.get("DB_PORT", "3306")),
                           user=_os.environ["DB_USER"], password=_os.environ["DB_PASSWORD"],
                           database=dataset, ssl={"ssl": {}})  # the database accepts encrypted connections only
    return _SaturnDB(conn)

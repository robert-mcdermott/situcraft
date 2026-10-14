# Writes the Parquet files the tests read: the same 2,500 rows (formulas the Go test recomputes)
# written by DuckDB and by pyarrow with each codec, page version and encoding a reader's code
# depends on, in row groups of 1,000 so that every file has several.
#
#   uv run --with duckdb --with pyarrow python -I make_fixtures.py
import os
import duckdb
import pyarrow as pa
import pyarrow.parquet as pq

here = os.path.dirname(os.path.abspath(__file__))
N = 2500
SELECT = f"""
SELECT i::BIGINT AS id,
       ((i * 7) % 1000 - 500)::INTEGER AS small,
       (i % 256)::UTINYINT AS u8,
       (i / 3.0)::FLOAT AS f,
       (i * 0.25 - 100)::DOUBLE AS d,
       'gene_' || (i % 97) AS s,
       (i % 3 = 0) AS b,
       CASE WHEN i % 5 = 0 THEN NULL ELSE i::INTEGER END AS maybe,
       CASE WHEN i % 7 = 0 THEN NULL ELSE 'cell-' || i END AS name
FROM range({N}) t(i) ORDER BY i
"""
con = duckdb.connect()
for codec in ['zstd', 'snappy', 'gzip', 'uncompressed', 'lz4_raw']:
    con.execute(f"COPY ({SELECT}) TO '{here}/duckdb-{codec}.parquet' (FORMAT parquet, COMPRESSION {codec}, ROW_GROUP_SIZE 1000)")
con.execute(f"COPY ({SELECT}) TO '{here}/duckdb-v2.parquet' (FORMAT parquet, COMPRESSION zstd, ROW_GROUP_SIZE 1000, PARQUET_VERSION V2)")

table = con.execute(SELECT).arrow()
if not isinstance(table, pa.Table):
    table = table.read_all()
write = lambda name, **kw: pq.write_table(table, f'{here}/{name}.parquet', row_group_size=1000, **kw)
write('pyarrow-plain', use_dictionary=False, compression='snappy')
write('pyarrow-dictionary', compression='zstd')
write('pyarrow-v2pages', compression='zstd', data_page_version='2.0')
write('pyarrow-delta', use_dictionary=False, compression='none', column_encoding={
    'id': 'DELTA_BINARY_PACKED', 'small': 'DELTA_BINARY_PACKED', 'maybe': 'DELTA_BINARY_PACKED',
    's': 'DELTA_BYTE_ARRAY', 'name': 'DELTA_LENGTH_BYTE_ARRAY'})
write('pyarrow-bytestreamsplit', use_dictionary=False, compression='gzip', column_encoding={'f': 'BYTE_STREAM_SPLIT', 'd': 'BYTE_STREAM_SPLIT'})
write('pyarrow-lz4', compression='lz4')
write('pyarrow-brotli', compression='brotli')
print('written; duckdb', duckdb.__version__, 'pyarrow', pa.__version__)

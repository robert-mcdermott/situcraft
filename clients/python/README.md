# situcraft (Python)

Remote control of a running [SituCraft](https://github.com/robert-mcdermott/situcraft) from
Python: every action an AI agent has is a method, with its arguments and help. The standard
library only; pandas for data frames.

```bash
pip install "git+https://github.com/robert-mcdermott/situcraft#subdirectory=clients/python"
```

```python
import situcraft

sc = situcraft.connect()          # SituCraft started with --remote-control
sc.open_example("simulated")
cells = sc.query_cells({"attribute": "transcripts", "min": 50}, limit=0).frame("rows")
sc.show_tissue(genes=["SIM001"], file="tissue.jpg")
```

See [the clients' README](../README.md) for connecting, arguments, tables, pictures and errors.

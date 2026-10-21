# situcraft (R)

Remote control of a running [SituCraft](https://github.com/robert-mcdermott/situcraft) from R:
every action an AI agent has is a function (`sc_` and the action's name), with its arguments and
help. It needs curl and jsonlite.

```r
remotes::install_github("robert-mcdermott/situcraft", subdir = "clients/r")
```

```r
library(situcraft)
sc_connect()                      # SituCraft started with --remote-control
sc_open_example("simulated")
cells <- as.data.frame(sc_query_cells(list(attribute = "transcripts", min = 50), limit = 0), key = "rows")
sc_show_tissue(genes = "SIM001", file = "tissue.jpg")
```

See [the clients' README](../README.md) for connecting, arguments, tables, pictures and errors.

# The R client against a running SituCraft (wave 9, slice 7): the simulated tissue opened, clustered
# and labeled through the client; its tables as data frames, each column of one type; the tissue and
# a map written as files; results equal to the HTTP API's for the same actions; what an action
# changed; errors carrying SituCraft's reason; and SituCraft found through its connection file.
# clients/test-clients.mjs starts SituCraft and sets SITUCRAFT_TEST_URL, SITUCRAFT_TEST_TOKEN,
# SITUCRAFT_TEST_DIR and SITUCRAFT_DATA_DIR; without them the tests that need SituCraft are skipped.
# Adapted from MaveScape 0.8.0's tests (the same author's).

url <- Sys.getenv("SITUCRAFT_TEST_URL")
token <- Sys.getenv("SITUCRAFT_TEST_TOKEN")
out <- Sys.getenv("SITUCRAFT_TEST_DIR", tempdir())
live <- function() skip_if(!nzchar(url), "needs a running SituCraft (node clients/test-clients.mjs)")
png <- as.raw(c(0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a))
jpeg <- as.raw(c(0xff, 0xd8, 0xff))

# The same action sent to the HTTP API directly, parsed as the client parses answers.
http <- function(action, args = structure(list(), names = character(0))) {
  handle <- curl::new_handle()
  curl::handle_setheaders(handle, "Content-Type" = "application/json", "X-SituCraft-Token" = token)
  curl::handle_setopt(handle, customrequest = "POST", postfields = jsonlite::toJSON(list(action = action, args = args, client = "R"), auto_unbox = TRUE))
  response <- curl::curl_fetch_memory(paste0(url, "/api/remote/action"), handle = handle)
  text <- rawToChar(response$content)
  Encoding(text) <- "UTF-8"
  jsonlite::fromJSON(text)
}

# The simulated tissue, clustered and labeled once for the tests that need it.
setup <- local({
  done <- NULL
  function() {
    if (is.null(done)) {
      sc_connect(client = "R")
      sc_open_example("simulated")
      project <- sc_get_state()$data$project$name
      sc_cluster_cells()
      labels <- sc_label_cells(markers = "simulated")$data$id
      done <<- list(project = project, labels = labels)
    }
    done
  }
})

test_that("it connects through the connection file, and its version is the program's", {
  live()
  sc <- sc_connect(client = "R")
  expect_equal(sc$url, url)
  expect_equal(sc$token, token)
  expect_equal(sc$version, as.character(utils::packageVersion("situcraft")))
  expect_equal(sc$api, situcraft:::.sc_api)
  expect_output(print(sc), "SituCraft")
})

test_that("every action is a function", {
  live()
  names <- sc_tools()$name
  expect_gt(length(names), 80)
  missing <- names[!vapply(paste0("sc_", names), exists, logical(1), envir = asNamespace("situcraft"))]
  expect_equal(missing, character(0))
})

test_that("tables come back as data frames, each column of one type", {
  live()
  s <- setup()
  cells <- sc_query_cells(list(labels = s$labels, `in` = list("epithelial-like")), project = s$project, by = s$labels, columns = list("transcripts"), limit = 0)
  rows <- as.data.frame(cells, key = "rows")
  expect_equal(names(rows), c("row", "id", "x", "y", "transcripts"))
  expect_equal(nrow(rows), cells$data$kept)
  expect_type(rows$transcripts, "integer")
  expect_type(rows$x, "double")
  expect_type(rows$id, "character")
  composition <- as.data.frame(cells)
  expect_equal(sum(composition$kept), cells$data$kept)
  listed <- as.data.frame(sc_describe_project(project = s$project), key = "results")
  expect_true(s$labels %in% listed$id)
  numbers <- sc_get_result(s$labels, limit = 0)
  table <- as.data.frame(numbers)
  expect_equal(names(table), unlist(numbers$data$table$columns))
  expect_equal(nrow(table), numbers$data$table$total)
})

test_that("results equal the HTTP API's", {
  live()
  s <- setup()
  mine <- sc_get_result(s$labels, limit = 3)
  theirs <- http("get_result", list(id = s$labels, limit = 3))
  expect_true(theirs$ok)
  expect_equal(mine$message, theirs$message)
  expect_equal(mine$data, theirs$data)
  mine <- sc_describe_project(project = s$project)
  theirs <- http("describe_project", list(project = s$project))
  expect_equal(mine$message, theirs$message)
  expect_equal(mine$data, theirs$data)
})

test_that("pictures are written as files", {
  live()
  setup()
  tissue <- file.path(out, "r-tissue.jpg")
  result <- sc_show_tissue(file = tissue, picture_width = 400)
  expect_equal(readBin(tissue, "raw", 3), jpeg)
  expect_equal(attr(sc_images(result)[[1]], "type"), "jpeg")
  drawn <- file.path(out, "r-map.png")
  sc_show_map(file = drawn, picture_width = 400)
  expect_equal(readBin(drawn, "raw", 8), png)
})

test_that("a result says what it changed in the project", {
  live()
  s <- setup()
  saved <- sc_save_filter("R epithelial-like", where = list(labels = s$labels, `in` = list("epithelial-like")))
  expect_equal(saved$changed$kind, "filter")
  expect_equal(saved$changed$change, "added")
})

test_that("errors carry SituCraft's reason", {
  live()
  s <- setup()
  error <- tryCatch(sc_call("no_such_action"), situcraft_error = function(e) e)
  expect_match(conditionMessage(error), "Unknown action")
  error <- tryCatch(sc_query_cells(list(attribute = "size", min = 1), project = s$project), situcraft_error = function(e) e)
  expect_match(conditionMessage(error), "no attribute \"size\"")
  expect_equal(error$action, "query_cells")
  path <- file.path(out, "r-methods.md")
  unlink(path)
  without <- sc_connect(url, "")
  error <- tryCatch(sc_export("methods", path, project = TRUE, sc = without), situcraft_error = function(e) e)
  expect_equal(error$status, 401)
  sc_connect(client = "R")
  gone <- tryCatch(sc_connect("http://127.0.0.1:9", ""), situcraft_error = function(e) e)
  expect_equal(gone$status, 0)
  sc_connect(client = "R")
})

test_that("the connection file and the environment", {
  saved <- Sys.getenv(c("SITUCRAFT_URL", "SITUCRAFT_TOKEN", "SITUCRAFT_DATA_DIR"), unset = NA)
  on.exit(for (k in names(saved)) if (is.na(saved[[k]])) Sys.unsetenv(k) else do.call(Sys.setenv, stats::setNames(list(saved[[k]]), k)))
  Sys.unsetenv(c("SITUCRAFT_URL", "SITUCRAFT_TOKEN", "SITUCRAFT_DATA_DIR"))
  folder <- tempfile()
  dir.create(folder)
  expect_equal(situcraft:::.sc_find(folder), list(url = "http://127.0.0.1:8840", token = NULL))
  writeLines('{"url": "http://127.0.0.1:8899", "token": "abc", "version": "x", "pid": 1}', file.path(folder, "remote.json"))
  expect_equal(situcraft:::.sc_find(folder), list(url = "http://127.0.0.1:8899", token = "abc"))
  Sys.setenv(SITUCRAFT_URL = "http://127.0.0.1:8900")
  expect_equal(situcraft:::.sc_find(folder)$url, "http://127.0.0.1:8900")
})

test_that("tables of columns and rows, records, and pictures, read as SituCraft writes them", {
  text <- '{"picture": {"caption": "The tissue", "image": "data:image/jpeg;base64,/9j/4AAQ"}, "pairs": [{"a": "x", "b": "y"}], "table": {"name": "pairs", "columns": ["a", "z"], "rows": [["x", 1.5], ["y", null]]}, "genes": {"columns": ["gene", "count"], "abundant": [["SIM001", 9]], "enriched": [["SIM033", 4]]}}'
  result <- structure(list(action = "test", message = "", data = jsonlite::fromJSON(text), raw = jsonlite::fromJSON(text, simplifyVector = FALSE)), class = "situcraft_result")
  first <- as.data.frame(result)
  expect_equal(first$a, c("x", "y"))
  expect_equal(first$z, c(1.5, NA))
  expect_equal(as.data.frame(result, key = "pairs")$b, "y")
  enriched <- as.data.frame(result, key = "genes.enriched")
  expect_equal(names(enriched), c("gene", "count"))
  expect_equal(enriched$count, 4L)
  pictures <- sc_images(result)
  expect_equal(names(pictures), "The tissue")
  expect_equal(attr(pictures[[1]], "type"), "jpeg")
  expect_error(as.data.frame(result, key = "nothing"), "has no nothing")
})

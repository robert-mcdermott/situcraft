# Remote control of a running SituCraft: the connection, any action by its name, results as data
# frames and pictures as files. The help pages are in man/; NAMESPACE and the action functions
# (tools.R) are written by clients/generate.mjs. Adapted from MaveScape 0.8.0's R client (the same
# author's).

.sc <- new.env(parent = emptyenv())
.picture <- "^data:image/(png|jpeg);base64,"

# Connect to a running SituCraft
#
# SituCraft started with --remote-control accepts actions from programs on this computer.
# Without arguments, the connection is found in the environment (SITUCRAFT_URL,
# SITUCRAFT_TOKEN) or in the remote.json file SituCraft writes to its data folder
# (data_dir, SITUCRAFT_DATA_DIR or the default folder).
sc_connect <- function(url = NULL, token = NULL, client = "R", timeout = 3600, data_dir = NULL) {
  if (is.null(url) || is.null(token)) {
    found <- .sc_find(data_dir)
    if (is.null(url)) url <- found$url
    if (is.null(token)) token <- found$token
  }
  sc <- structure(list(url = sub("/+$", "", url), token = token, client = client, timeout = timeout), class = "situcraft")
  info <- .sc_request(sc, "GET", "/api/remote/tools")$simple
  sc$version <- info$version
  sc$api <- info$api
  if (!is.null(info$api) && info$api != .sc_api) {
    warning(sprintf("SituCraft %s has version %d of its actions; this client was made for version %d: update %s. An action one of them lacks fails with SituCraft's reason.",
      info$version, as.integer(info$api), .sc_api, if (info$api > .sc_api) "the client" else "SituCraft"), call. = FALSE)
  }
  .sc$default <- sc
  sc
}

# The connection the functions use by default: the last one sc_connect() made.
sc_default <- function() {
  if (is.null(.sc$default)) sc_connect() else .sc$default
}

# Perform any SituCraft action by its name
#
# Arguments are named as SituCraft names them; NULL arguments are left out. The action is .action,
# so that no argument's name is taken for it by R's partial matching.
sc_call <- function(.action, ..., .args = list(), sc = sc_default()) {
  action <- .action
  args <- c(list(...), .args)
  args <- args[!vapply(args, is.null, logical(1))]
  if (!length(args)) args <- structure(list(), names = character(0))
  body <- jsonlite::toJSON(list(action = action, args = args, client = sc$client), auto_unbox = TRUE, null = "null", digits = NA)
  answer <- .sc_request(sc, "POST", "/api/remote/action", body, action)
  simple <- answer$simple
  if (!isTRUE(simple$ok)) {
    stop(.sc_error(if (!is.null(simple$message)) simple$message else paste(action, "failed"), 200, action))
  }
  structure(list(action = action, message = simple$message, data = simple$data, changed = simple$changed, raw = answer$raw$data), class = "situcraft_result")
}

# The actions this SituCraft performs
sc_tools <- function(sc = sc_default()) {
  info <- .sc_request(sc, "GET", "/api/remote/tools")$simple
  info$tools[, c("name", "title", "description")]
}

# The pictures in a result (show_tissue, show_map and figure with picture = TRUE), in order: a list
# of raw vectors (the tissue's a JPEG, a map's and a figure's a PNG), named by their captions,
# each with its type as an attribute.
sc_images <- function(result) {
  found <- list()
  captions <- character(0)
  walk <- function(value, caption) {
    if (is.list(value)) {
      own <- caption
      for (key in c("caption", "title", "name")) {
        if (!is.null(names(value)) && is.character(value[[key]]) && length(value[[key]]) == 1 && nzchar(value[[key]])) {
          own <- value[[key]]
          break
        }
      }
      for (item in value) walk(item, own)
    } else if (is.character(value)) {
      for (text in value) {
        if (!is.na(text) && grepl(.picture, text)) {
          type <- sub(paste0(.picture, ".*"), "\\1", substr(text, 1, 30))
          bytes <- jsonlite::base64_dec(sub(.picture, "", text))
          attr(bytes, "type") <- type
          found[[length(found) + 1]] <<- bytes
          captions[[length(captions) + 1]] <<- caption
        }
      }
    }
  }
  walk(result$raw, "")
  names(found) <- captions
  found
}

# Save a picture of a result to a file
sc_save_image <- function(result, file, index = NULL) {
  found <- sc_images(result)
  if (!length(found)) stop(.sc_error(paste(result$action, "returned no picture (show_tissue, show_map and figure give one with picture = TRUE)"), 0, result$action))
  if (is.null(index) && length(found) > 1) stop(.sc_error(paste0(result$action, " returned ", length(found), " pictures: give index (from 1)"), 0, result$action))
  bytes <- found[[if (is.null(index)) 1 else index]]
  attr(bytes, "type") <- NULL
  writeBin(bytes, file)
  invisible(file)
}

print.situcraft <- function(x, ...) {
  cat("<SituCraft ", x$version, " at ", x$url, if (is.null(x$token) || !nzchar(x$token)) " (no token)", ">\n", sep = "")
  invisible(x)
}

print.situcraft_result <- function(x, ...) {
  cat(x$action, ": ", x$message, "\n", sep = "")
  invisible(x)
}

# A table in a result's data as a data frame: data[[key]] (a dotted key for one inside another,
# such as "genes.enriched", whose columns are its parent's), or the first found: a table (columns
# and rows) before a list of records, the data's own fields before those inside them. Each column
# keeps its type: numbers as numbers, NA where SituCraft gave null.
as.data.frame.situcraft_result <- function(x, row.names = NULL, optional = FALSE, key = NULL, ...) {
  found <- .sc_find_table(x$raw, key, x$action)
  frame <- .sc_frame(found$value, found$columns)
  if (!is.null(row.names)) row.names(frame) <- row.names
  frame
}

# --- Internals ------------------------------------------------------------------------------------

# Arrays stay arrays in JSON, even of one element.
.sc_array <- function(x) if (is.null(x)) NULL else as.list(x)

.sc_error <- function(message, status, action) {
  structure(class = c("situcraft_error", "error", "condition"), list(message = message, call = NULL, status = status, action = action))
}

.sc_is_table <- function(v) is.list(v) && !is.null(names(v)) && is.list(v$columns) && is.list(v$rows) && all(vapply(v$rows, is.list, logical(1)))
.sc_is_records <- function(v) is.list(v) && is.null(names(v)) && length(v) > 0 && all(vapply(v, function(r) is.list(r) && !is.null(names(r)), logical(1)))

.sc_find_table <- function(data, key, action) {
  if (!is.null(key)) {
    parent <- NULL
    value <- data
    for (part in strsplit(key, ".", fixed = TRUE)[[1]]) {
      if (!is.list(value) || is.null(names(value)) || !(part %in% names(value))) stop(paste0(action, "'s data has no ", key))
      parent <- value
      value <- value[[part]]
    }
    if (is.list(value) && is.null(names(value)) && length(value) && all(vapply(value, is.list, logical(1))) && !is.null(parent$columns) && !.sc_is_records(value)) {
      return(list(value = value, columns = unlist(parent$columns)))
    }
    return(list(value = value, columns = NULL))
  }
  if (.sc_is_table(data) || .sc_is_records(data)) return(list(value = data, columns = NULL))
  level <- list(data)
  while (length(level)) {
    for (wanted in list(.sc_is_table, .sc_is_records)) {
      for (d in level) for (v in d) if (wanted(v)) return(list(value = v, columns = NULL))
    }
    level <- unlist(lapply(level, function(d) Filter(function(v) is.list(v) && !is.null(names(v)), d)), recursive = FALSE)
  }
  stop(paste0(action, " returned no table; its data has ", paste(names(data), collapse = ", ")))
}

# A column of values (NULL as NA): one type when each is a number, a string or a logical, else a
# list.
.sc_column <- function(values) {
  values <- lapply(values, function(v) if (is.null(v)) NA else v)
  if (all(vapply(values, function(v) is.atomic(v) && length(v) == 1, logical(1)))) return(unlist(values))
  I(values)
}

.sc_frame <- function(value, columns = NULL) {
  if (.sc_is_table(value)) {
    columns <- unlist(value$columns)
    value <- value$rows
  }
  if (!is.null(columns)) {
    frame <- lapply(seq_along(columns), function(j) .sc_column(lapply(value, function(row) if (j <= length(row)) row[[j]] else NULL)))
  } else {
    if (!.sc_is_records(value)) stop("not a table")
    columns <- unique(unlist(lapply(value, names)))
    frame <- lapply(columns, function(name) .sc_column(lapply(value, function(row) row[[name]])))
  }
  names(frame) <- columns
  as.data.frame(frame, stringsAsFactors = FALSE, check.names = FALSE, optional = TRUE)
}

.sc_default_data_dir <- function() {
  home <- path.expand("~")
  if (.Platform$OS.type == "windows") {
    base <- Sys.getenv("APPDATA")
    return(if (nzchar(base)) file.path(base, "SituCraft") else file.path(home, ".situcraft"))
  }
  if (Sys.info()[["sysname"]] == "Darwin") return(file.path(home, "Library", "Application Support", "SituCraft"))
  base <- Sys.getenv("XDG_CONFIG_HOME")
  file.path(if (nzchar(base)) base else file.path(home, ".config"), "SituCraft")
}

.sc_find <- function(data_dir = NULL) {
  url <- Sys.getenv("SITUCRAFT_URL")
  token <- Sys.getenv("SITUCRAFT_TOKEN")
  token <- if (nzchar(token)) token else NULL
  if (nzchar(url)) return(list(url = url, token = token))
  folder <- if (!is.null(data_dir)) data_dir else if (nzchar(Sys.getenv("SITUCRAFT_DATA_DIR"))) Sys.getenv("SITUCRAFT_DATA_DIR") else .sc_default_data_dir()
  path <- file.path(folder, "remote.json")
  info <- tryCatch(jsonlite::fromJSON(path), error = function(e) NULL, warning = function(w) NULL)
  if (is.null(info$url)) return(list(url = "http://127.0.0.1:8840", token = token))
  list(url = info$url, token = if (!is.null(token)) token else info$token)
}

# A request's answer, read twice: simplified (vectors, data frames) for the fields, and as written
# (lists) for the tables, whose columns keep their types.
.sc_request <- function(sc, method, path, body = NULL, action = NULL) {
  handle <- curl::new_handle(timeout = sc$timeout)
  headers <- c(Accept = "application/json")
  if (!is.null(sc$token) && nzchar(sc$token)) headers[["X-SituCraft-Token"]] <- sc$token
  if (!is.null(body)) {
    headers[["Content-Type"]] <- "application/json"
    curl::handle_setopt(handle, customrequest = method, postfields = enc2utf8(as.character(body)))
  }
  do.call(curl::handle_setheaders, c(list(handle), as.list(headers)))
  response <- tryCatch(curl::curl_fetch_memory(paste0(sc$url, path), handle = handle), error = function(e) {
    stop(.sc_error(paste0("SituCraft is not running at ", sc$url, " (", conditionMessage(e), "). Start it with: situcraft --remote-control"), 0, action))
  })
  text <- rawToChar(response$content)
  Encoding(text) <- "UTF-8"
  simple <- tryCatch(jsonlite::fromJSON(text, simplifyVector = TRUE), error = function(e) NULL)
  if (response$status_code >= 400) {
    message <- if (!is.null(simple$error)) simple$error else text
    if (response$status_code == 404 && startsWith(path, "/api/remote/")) message <- paste0(sc$url, " does not accept actions: start SituCraft with --remote-control.")
    stop(.sc_error(message, response$status_code, action))
  }
  if (is.null(simple)) stop(.sc_error(paste("SituCraft answered with something other than JSON:", substr(text, 1, 200)), response$status_code, action))
  raw <- if (is.null(body)) NULL else jsonlite::fromJSON(text, simplifyVector = FALSE)
  list(simple = simple, raw = raw)
}

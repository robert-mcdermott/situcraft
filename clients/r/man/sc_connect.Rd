\name{sc_connect}
\alias{sc_connect}
\alias{sc_default}
\title{Connect to a running SituCraft}
\usage{
sc_connect(url = NULL, token = NULL, client = "R", timeout = 3600, data_dir = NULL)

sc_default()
}
\arguments{
\item{url}{SituCraft's address, such as \code{"http://127.0.0.1:8840"}.}
\item{token}{The token SituCraft printed when it started; opening files by path and writing files need it.}
\item{client}{The name SituCraft's history and log of calls show for your actions.}
\item{timeout}{Seconds to wait for an action (a refinement or a cohort's steps take a while).}
\item{data_dir}{SituCraft's data folder, when it was started with \code{--data-dir}.}
}
\value{
A \code{situcraft} connection, which also becomes the default that every function uses.
}
\description{
SituCraft started with \code{situcraft --remote-control} accepts actions from programs on this
computer. Every action an AI agent has in SituCraft is an R function (\code{sc_open_example},
\code{sc_cluster_cells}, \code{sc_query_cells}, \code{sc_get_result}, \code{sc_show_tissue},
\dots) that the open SituCraft window performs, so the analysis stays visible; each change goes into
the project's history, and every call into the library's log.

Without \code{url} and \code{token}, the connection is found in the environment
(\code{SITUCRAFT_URL} and \code{SITUCRAFT_TOKEN}), else in the \code{remote.json} file SituCraft
writes to its data folder (\code{data_dir}, \code{SITUCRAFT_DATA_DIR} or the default folder), else
at SituCraft's default address without a token. \code{sc_default()} returns the last connection
made, connecting first if there is none.
}
\examples{
\dontrun{
sc_connect()
sc_open_example("simulated")
cells <- as.data.frame(sc_query_cells(list(attribute = "transcripts", min = 50), limit = 0), key = "rows")
sc_show_tissue(file = "tissue.jpg")
}
}

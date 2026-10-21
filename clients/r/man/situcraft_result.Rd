\name{situcraft_result}
\alias{situcraft_result}
\alias{print.situcraft_result}
\alias{as.data.frame.situcraft_result}
\alias{print.situcraft}
\title{SituCraft's answer to an action}
\usage{
\method{print}{situcraft_result}(x, ...)

\method{as.data.frame}{situcraft_result}(x, row.names = NULL, optional = FALSE, key = NULL, ...)

\method{print}{situcraft}(x, ...)
}
\arguments{
\item{x}{A \code{situcraft_result}, or a \code{situcraft} connection.}
\item{row.names, optional}{As for \code{\link{as.data.frame}}.}
\item{key}{The table in the result's data to convert, by its name (a dotted name for one inside another, such as \code{"genes.enriched"}); by default the first table in it.}
\item{...}{Not used.}
}
\value{
\code{print} returns \code{x} invisibly; \code{as.data.frame} a data frame, each column of one
type, \code{NA} where SituCraft gave null.
}
\description{
Every action returns a \code{situcraft_result}, a list of the \code{action}, SituCraft's
\code{message}, its \code{data} and what it \code{changed} in the project. Printing shows the
message; \code{as.data.frame} gives a table in the data: SituCraft gives a table as its
\code{columns} and \code{rows} (a result's numbers from \code{\link{sc_get_result}}, the cells
\code{\link{sc_query_cells}} keeps), or as a list of records (the results
\code{\link{sc_describe_project}} lists). Without \code{key}, the first table found is taken, a
table of columns and rows before a list of records.
}

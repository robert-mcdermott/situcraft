\name{sc_tools}
\alias{sc_tools}
\title{The actions a running SituCraft performs}
\usage{
sc_tools(sc = sc_default())
}
\arguments{
\item{sc}{A connection from \code{\link{sc_connect}} (default: the last one made).}
}
\value{
A data frame of the actions' names, titles and descriptions.
}
\description{
The actions of the running SituCraft (an AI agent's tools), each also an R function named
\code{sc_<name>}.
}

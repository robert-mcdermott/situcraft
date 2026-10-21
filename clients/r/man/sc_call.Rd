\name{sc_call}
\alias{sc_call}
\title{Perform any SituCraft action by its name}
\usage{
sc_call(.action, ..., .args = list(), sc = sc_default())
}
\arguments{
\item{.action}{The action's name, as \code{\link{sc_tools}} lists it (for example \code{"get_state"}); named so that no argument of the action is taken for it.}
\item{...}{The action's arguments, named as SituCraft names them (such as \code{min_quality}); \code{NULL} arguments are left out.}
\item{.args}{More arguments, as a named list.}
\item{sc}{A connection from \code{\link{sc_connect}} (default: the last one made).}
}
\value{
A \code{situcraft_result}: the \code{action}, SituCraft's \code{message} (a sentence), its
\code{data}, and what the action \code{changed} in the project. An action SituCraft refuses or
cannot perform is an error of class \code{situcraft_error} carrying SituCraft's reason, with the
HTTP \code{status} and the \code{action}.
}
\description{
Every \code{sc_*} action function calls \code{sc_call}; it is the way to reach an action this
version of the package does not know yet.
}
\seealso{\code{\link{situcraft_result}}}

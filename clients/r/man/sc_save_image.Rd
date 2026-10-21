\name{sc_save_image}
\alias{sc_save_image}
\alias{sc_images}
\title{Pictures of a result, as files}
\usage{
sc_save_image(result, file, index = NULL)

sc_images(result)
}
\arguments{
\item{result}{The result of \code{\link{sc_show_tissue}}, \code{\link{sc_show_map}} or \code{\link{sc_figure}}, each with \code{picture = TRUE}.}
\item{file}{The path of the file to write: the tissue's picture is a JPEG, a map's and a figure's a PNG.}
\item{index}{Which picture, from 1, when the result has several.}
}
\value{
\code{sc_save_image}: \code{file}, invisibly. \code{sc_images}: a list of the pictures as raw
vectors, named by their captions, each with its \code{type} (\code{"jpeg"} or \code{"png"}) as an
attribute.
}
\description{
SituCraft gives pictures as data URLs in a result's data. \code{sc_save_image} writes one to a
file; \code{sc_show_tissue(file = )}, \code{sc_show_map(file = )} and \code{sc_figure(file = )} do
the same.
}

\name{situcraft-package}
\alias{situcraft-package}
\alias{situcraft}
\docType{package}
\title{Remote control of a running SituCraft}
\description{
SituCraft is a workbench for imaging spatial transcriptomics (10x Genomics Xenium output) that runs
on this computer. Started with \code{situcraft --remote-control}, it performs actions sent by
programs on the same computer in its open window. This package makes each action an R function:
\code{\link{sc_connect}} finds the running SituCraft, and \code{sc_open_example},
\code{sc_cluster_cells}, \code{sc_label_cells}, \code{sc_spatial_analysis}, \code{sc_query_cells},
\code{sc_get_result}, \code{sc_show_tissue}, \code{sc_export} and the others do what their names
say. \code{\link{sc_tools}} lists them, and \code{\link{sc_call}} sends any action by its name.
SituCraft's results are research findings about a tissue, never a diagnosis.
}
\seealso{\url{https://github.com/robert-mcdermott/situcraft}}

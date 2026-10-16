# SingleR (Aran D, et al. Reference-based analysis of lung single-cell sequencing reveals a
# transitional profibrotic macrophage. Nat Immunol 2019;20:163–172. doi:10.1038/s41590-018-0276-y)
# on the cells SituCraft labeled from a reference (wave 4, slice 6; research.md §5.6), run as a
# separate program for comparison. The cells' counts are those SituCraft wrote
# (<data>/references/<set>/counts.mtx), log-normalized as SituCraft's clustering does (each cell
# scaled to the median total, then log1p); the reference is restricted to the genes both have.
#
# Twice: given the same profiles SituCraft compared (each label's mean log-expression: the atlas's
# hpca-main.csv, or the simulated reference's cells averaged by type as SituCraft averages them),
# and at SingleR's defaults on the reference's own samples or cells. Writes into the set's folder:
#   singler-aggregated.csv  each cell's label, pruned label and first-round score for every label
#   singler-default.csv     each cell's label and pruned label
#   singler.json            the versions
#
# Usage: Rscript singler.R <data folder> <set>   (run by compare.sh)
args <- commandArgs(trailingOnly = TRUE)
data <- args[1]
set <- args[2]
folder <- file.path(data, "references", set)
suppressPackageStartupMessages({
  library(Matrix)
  library(SingleR)
})

lognorm <- function(counts) {
  totals <- Matrix::colSums(counts)
  scale <- totals / median(totals)
  scale[scale == 0] <- 1
  out <- counts %*% Diagonal(x = 1 / scale)
  out@x <- log1p(out@x)
  dimnames(out) <- dimnames(counts)
  out
}

read_counts <- function(dir) {
  m <- as(readMM(file.path(dir, "counts.mtx")), "CsparseMatrix")
  rownames(m) <- readLines(file.path(dir, "genes.txt"))
  colnames(m) <- readLines(file.path(dir, "cells.txt"))
  m
}

query <- lognorm(read_counts(folder))
if (set == "simulated-standard") {
  ref_dir <- file.path(folder, "reference")
  ref_cells <- lognorm(read_counts(ref_dir))
  ref_labels <- readLines(file.path(ref_dir, "labels.txt"))
  types <- sort(unique(ref_labels))
  profiles <- sapply(types, function(l) Matrix::rowMeans(ref_cells[, ref_labels == l, drop = FALSE]))
  colnames(profiles) <- types
  samples <- ref_cells
  sample_labels <- ref_labels
} else {
  table <- read.csv(file.path(data, "references", "hpca-main.csv"), check.names = FALSE)
  profiles <- as.matrix(table[, -1])
  rownames(profiles) <- table$gene
  hpca <- readRDS(file.path(data, "references", "hpca-samples.rds"))
  samples <- hpca$logcounts
  sample_labels <- hpca$labels
}
common <- intersect(rownames(query), rownames(profiles))
cat(sprintf("%s: %d cells, %d genes shared with the reference's %d; %d labels\n", set, ncol(query), length(common), nrow(profiles), ncol(profiles)))

aggregated <- SingleR(test = query[common, ], ref = profiles[common, , drop = FALSE], labels = colnames(profiles))
scores <- as.data.frame(aggregated$scores)
out <- data.frame(cell = colnames(query), label = aggregated$labels, pruned = aggregated$pruned.labels, scores, check.names = FALSE)
write.csv(out, file.path(folder, "singler-aggregated.csv"), row.names = FALSE)

common2 <- intersect(rownames(query), rownames(samples))
default <- SingleR(test = query[common2, ], ref = samples[common2, ], labels = sample_labels)
write.csv(data.frame(cell = colnames(query), label = default$labels, pruned = default$pruned.labels), file.path(folder, "singler-default.csv"), row.names = FALSE)

versions <- list(R = paste(R.version$major, R.version$minor, sep = "."), SingleR = as.character(packageVersion("SingleR")), Matrix = as.character(packageVersion("Matrix")))
if (set != "simulated-standard") versions$celldex <- as.character(packageVersion("celldex"))
writeLines(sprintf("{%s}", paste(sprintf("\"%s\": \"%s\"", names(versions), unlist(versions)), collapse = ", ")), file.path(folder, "singler.json"))
cat(sprintf("aggregated: %d labels taken; default: %d\n", sum(!is.na(aggregated$labels)), sum(!is.na(default$labels))))

# The Human Primary Cell Atlas reference (Mabbott NA, et al. An expression atlas of human primary
# cells: inference of gene function from coexpression networks. BMC Genomics 2013;14:632.
# doi:10.1186/1471-2164-14-632), as celldex packages it (Aran D, et al. 2019), for comparing
# SituCraft's labels from a reference with SingleR's (wave 4, slice 6; research.md §5.6).
#
# Writes into <out>:
#   hpca-samples.rds   the samples' log-expression and main labels (for SingleR's own defaults)
#   hpca-main.csv      each main label's profile: its samples' mean log-expression, a row per gene
#                      (what SituCraft reads, and what SingleR is given as an aggregated reference)
#
# Usage: Rscript hpca.R <out folder>   (run by compare.sh with the library in validation/cache)
args <- commandArgs(trailingOnly = TRUE)
out <- args[1]
dir.create(out, showWarnings = FALSE, recursive = TRUE)
suppressPackageStartupMessages({
  library(celldex)
  library(SummarizedExperiment)
})
ref <- HumanPrimaryCellAtlasData()
x <- assay(ref, "logcounts")
labels <- ref$label.main
saveRDS(list(logcounts = x, labels = labels), file.path(out, "hpca-samples.rds"))
groups <- sort(unique(labels))
profiles <- sapply(groups, function(l) rowMeans(x[, labels == l, drop = FALSE]))
colnames(profiles) <- groups
table <- data.frame(gene = rownames(x), profiles, check.names = FALSE)
write.csv(table, file.path(out, "hpca-main.csv"), row.names = FALSE, quote = TRUE)
cat(sprintf("%d samples, %d genes, %d main labels; values from %.3f to %.3f\n", ncol(x), nrow(x), length(groups), min(x), max(x)))

# A gene changed in one cell type, found from R (wave 9, slice 7).
#
# SituCraft's simulated cohort: eight sections of eight subjects, four control and four treated, on
# two slides. In the treated subjects' epithelial-like cells the simulator raised one gene, SIM033,
# 2.5 times; SituCraft is not told which. This script describes the design (the subject is the
# replicate), runs the same steps on every sample (cells clustered and labeled from the simulated
# tissue's marker set, a graph and its neighbor enrichment), summarizes the samples, and compares
# each label's expression between the conditions, gene by gene. In R it reads the 424 tests as a
# data frame, checks that SIM033 in the epithelial-like cells alone is found, draws a volcano and
# saves the tests and SituCraft's methods.
#
# Start SituCraft with remote control, then run this in a folder where it may write its results:
#
#   situcraft --remote-control
#   Rscript simulated-cohort.R
#
# It needs the situcraft package (clients/r), with curl and jsonlite.

library(situcraft)

invisible(sc_connect(client = "simulated-cohort.R"))
opened <- sc_open_example("simulated-cohort")
cat(opened$message, "\n")

# The subject is the replicate; the treated subjects are compared with the controls.
design <- sc_design(unit = "subject", group = "condition", reference = "control", save = "By condition")
cat(design$message, "\n")

# The same steps on every sample, then the samples summarized and their expression compared.
cat(sc_cohort_steps(markers = "simulated", questions = list("enrichment"))$message, "\n")
cat(sc_summarize_samples()$message, "\n")
compared <- sc_compare_expression(design = "By condition")
cat(compared$message, "\n")

# Every test as a data frame: each label's genes, their log2 fold changes with intervals and
# adjusted p-values.
tests <- as.data.frame(sc_get_result(compared$data$id, limit = 0), key = "table")
tests <- tests[tests$tested == "true", ]
cat(nrow(tests), "tests in", length(unique(tests$label)), "labels\n")
found <- tests[!is.na(tests$padj) & tests$padj < 0.05, ]
print(found[, c("label", "gene", "logFC", "lo", "hi", "padj")], row.names = FALSE)

# The planted change, and nothing else: SIM033 in the epithelial-like cells, its interval above 0
# (the true cells' own pseudobulk shows +0.97 log2 between the conditions).
stopifnot(
  nrow(found) == 1,
  found$gene == "SIM033",
  found$label == "epithelial-like",
  found$lo > 0,
  found$logFC > 0.5,
  nrow(tests) > 300
)

# A volcano of the epithelial-like cells' genes, SIM033 marked.
epithelial <- tests[tests$label == "epithelial-like", ]
png("epithelial-volcano.png", width = 900, height = 700, res = 120)
plot(epithelial$logFC, -log10(epithelial$p), pch = 19, col = ifelse(epithelial$gene == "SIM033", "firebrick", "grey55"),
  xlab = "log2 fold change, treated - control", ylab = "-log10 p", main = "Epithelial-like cells: treated against control")
with(epithelial[epithelial$gene == "SIM033", ], text(logFC, -log10(p), gene, pos = 2))
invisible(dev.off())

# The tests as a table, and the methods as SituCraft writes them.
utils::write.csv(tests, "expression-tests.csv", row.names = FALSE)
methods <- file.path(getwd(), "methods.md")
if (file.exists(methods)) file.remove(methods)
invisible(sc_export("methods", methods, result = compared$data$id))
stopifnot(file.exists("epithelial-volcano.png"), file.exists("expression-tests.csv"), file.exists(methods))
cat("Wrote epithelial-volcano.png, expression-tests.csv and methods.md.\n")

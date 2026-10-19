# The reference for SituCraft's moderated linear models of pseudobulk counts (wave 7, slice 7):
# each case of cases.json filtered by expression, normalized by TMM, made ready by voom and tested
# by limma's moderated t, as edgeR and limma do it; each case of plain values (no counts) by limma's
# linear models and moderated t alone; each lowess curve as R smooths it; and the special functions
# at a grid of points. Writes limma.json.
#
# Usage: Rscript compare.R cases.json limma.json
suppressPackageStartupMessages({
  library(limma)
  library(edgeR)
  library(jsonlite)
})

args <- commandArgs(trailingOnly = TRUE)
cases <- fromJSON(args[1], simplifyVector = TRUE, simplifyMatrix = TRUE)

test <- function(case) {
  counts <- as.matrix(case$counts)
  X <- as.matrix(case$x)
  y <- DGEList(counts)
  keep <- filterByExpr(y, design = X)
  y <- y[keep, , keep.lib.sizes = FALSE]
  y <- normLibSizes(y, method = "TMM")
  v <- voom(y, X)
  fit <- lmFit(v, X)
  contrasts <- as.matrix(case$contrasts)
  if (ncol(contrasts) == 1) contrasts <- t(contrasts)
  p <- ncol(X)
  tests <- lapply(seq_len(nrow(contrasts)), function(k) {
    # Each contrast a coefficient of the same design, reparametrized: the same fitted values,
    # voom weights and residual variances, and the contrast's exact standard error.
    cc <- contrasts[k, ]
    j <- which(cc != 0)[1]
    C <- diag(p)
    C[j, ] <- cc
    X2 <- X %*% solve(C)
    eb <- eBayes(lmFit(v$E, X2, weights = v$weights), legacy = TRUE)
    tt <- topTable(eb, coef = j, number = Inf, sort.by = "none", confint = 0.95)
    s <- function(v) signif(unname(v), 12)
    list(logFC = s(tt$logFC), lo = s(tt$CI.L), hi = s(tt$CI.R), t = s(tt$t), p = s(tt$P.Value),
         s2Prior = eb$s2.prior, dfPrior = eb$df.prior, dfTotal = eb$df.total[1])
  })
  list(name = case$name, keep = unname(as.logical(keep)), lib = y$samples$lib.size,
       factors = y$samples$norm.factors, span = v$span, weights = signif(unname(v$weights), 10),
       sigma = unname(fit$sigma), tests = tests)
}

# Plain values (features × rows) by lmFit and eBayes alone.
plain <- function(case) {
  Y <- as.matrix(case$y)
  X <- as.matrix(case$x)
  contrasts <- as.matrix(case$contrasts)
  if (ncol(contrasts) == 1) contrasts <- t(contrasts)
  p <- ncol(X)
  tests <- lapply(seq_len(nrow(contrasts)), function(k) {
    cc <- contrasts[k, ]
    j <- which(cc != 0)[1]
    C <- diag(p)
    C[j, ] <- cc
    eb <- eBayes(lmFit(Y, X %*% solve(C)), legacy = TRUE)
    tt <- topTable(eb, coef = j, number = Inf, sort.by = "none", confint = 0.95)
    v <- function(x) I(unname(x))  # arrays, even of one
    list(estimate = v(tt$logFC), lo = v(tt$CI.L), hi = v(tt$CI.R), t = v(tt$t), p = v(tt$P.Value),
         s2Prior = eb$s2.prior, dfPrior = eb$df.prior, dfTotal = eb$df.total[1])
  })
  list(name = case$name, tests = tests)
}

curve <- function(case) {
  l <- lowess(case$x, case$y, f = case$f, iter = case$iter, delta = case$delta)
  list(name = case$name, y = l$y)
}

tests <- lapply(seq_len(nrow(cases$tests)), function(i) test(lapply(cases$tests, function(col) col[[i]])))
plains <- lapply(seq_len(nrow(cases$plain)), function(i) plain(lapply(cases$plain, function(col) col[[i]])))
curves <- lapply(seq_len(nrow(cases$lowess)), function(i) curve(lapply(cases$lowess, function(col) col[[i]])))
grid <- c(0.01, 0.1, 0.5, 1, 1.5, 2, 3.7, 10, 19.9, 20, 25, 100, 1e4)
inverse <- c(1e-7, 1e-3, 0.01, 0.1, 0.5, 1, 2, 10, 100, 1e6, 1e8)
special <- list(x = grid, digamma = digamma(grid), trigamma = trigamma(grid),
                tetragamma = psigamma(grid, 2), inverseOf = inverse,
                trigammaInverse = trigammaInverse(inverse))
out <- list(software = list(R = R.version.string, limma = as.character(packageVersion("limma")),
                            edgeR = as.character(packageVersion("edgeR"))),
            tests = tests, plain = plains, lowess = curves, special = special)
writeLines(toJSON(out, digits = NA, auto_unbox = TRUE, matrix = "rowmajor"), args[2])

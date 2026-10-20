// The publications and software SituCraft cites (wave 8, slice 3; requirement E9), kept in one
// place: each with its authors, title, where it was published, its year, volume, pages and DOI.
// The program's jobs record their methods' citations as text (each artifact's lineage); a citation
// is found here by its first author as written and its year (cite, year). Each DOI is checked
// against a recorded copy of Crossref's record of it (validation/reference/crossref/, by
// web/lib/references.test.mjs). Vancouver style, as the methods paragraph writes them.

export const REFERENCES = {
  anselin1995: { cite: 'Anselin L', authors: ['Anselin L'], title: 'Local indicators of spatial association—LISA', journal: 'Geogr Anal', year: 1995, volume: '27', pages: '93–115', doi: '10.1111/j.1538-4632.1995.tb00338.x' },
  aran2019: { cite: 'Aran D', authors: ['Aran D', 'Looney AP', 'Liu L', 'et al.'], title: 'Reference-based analysis of lung single-cell sequencing reveals a transitional profibrotic macrophage', journal: 'Nat Immunol', year: 2019, volume: '20', pages: '163–172', doi: '10.1038/s41590-018-0276-y' },
  arthur2007: { cite: 'Arthur D', type: 'inproceedings', authors: ['Arthur D', 'Vassilvitskii S'], title: 'k-means++: the advantages of careful seeding', journal: 'Proceedings of the Eighteenth Annual ACM-SIAM Symposium on Discrete Algorithms (SODA)', year: 2007, pages: '1027–1035' },
  bankhead2017: { cite: 'Bankhead P', authors: ['Bankhead P', 'Loughrey MB', 'Fernández JA', 'et al.'], title: 'QuPath: open source software for digital pathology image analysis', journal: 'Sci Rep', year: 2017, volume: '7', pages: '16878', doi: '10.1038/s41598-017-17204-5' },
  benjamini1995: { cite: 'Benjamini Y', authors: ['Benjamini Y', 'Hochberg Y'], title: 'Controlling the false discovery rate: a practical and powerful approach to multiple testing', journal: 'J R Stat Soc B', year: 1995, volume: '57', pages: '289–300', doi: '10.1111/j.2517-6161.1995.tb02031.x' },
  besag1977: { cite: 'Besag J', authors: ['Besag J'], title: 'Contribution to the discussion of Dr Ripley\'s paper', journal: 'J R Stat Soc B', year: 1977, volume: '39', pages: '193–195' },
  bowyer1981: { cite: 'Bowyer A', authors: ['Bowyer A'], title: 'Computing Dirichlet tessellations', journal: 'Comput J', year: 1981, volume: '24', pages: '162–166', doi: '10.1093/comjnl/24.2.162' },
  chen2016: { cite: 'Chen Y', authors: ['Chen Y', 'Lun ATL', 'Smyth GK'], title: 'From reads to genes to pathways: differential expression analysis of RNA-Seq experiments using Rsubread and the edgeR quasi-likelihood pipeline', journal: 'F1000Research', year: 2016, volume: '5', pages: '1438', doi: '10.12688/f1000research.8987.2' },
  cliff1981: { cite: 'Cliff AD', type: 'book', authors: ['Cliff AD', 'Ord JK'], title: 'Spatial Processes: Models and Applications', publisher: 'Pion', year: 1981 },
  crowell2020: { cite: 'Crowell HL', authors: ['Crowell HL', 'Soneson C', 'Germain P-L', 'et al.'], title: 'muscat detects subpopulation-specific state transitions from multi-sample multi-condition single-cell transcriptomics data', journal: 'Nat Commun', year: 2020, volume: '11', pages: '6077', doi: '10.1038/s41467-020-19894-4' },
  diggle2003: { cite: 'Diggle PJ', type: 'book', authors: ['Diggle PJ'], title: 'Statistical Analysis of Spatial Point Patterns', edition: '2nd', publisher: 'Arnold', year: 2003 },
  dong2011: { cite: 'Dong W', type: 'inproceedings', authors: ['Dong W', 'Moses C', 'Li K'], title: 'Efficient k-nearest neighbor graph construction for generic similarity measures', journal: 'Proceedings of the 20th International Conference on World Wide Web (WWW)', year: 2011, pages: '577–586', doi: '10.1145/1963405.1963487' },
  ederer1974: { cite: 'Ederer F', authors: ['Ederer F', 'Mantel N'], title: 'Confidence limits on the ratio of two Poisson variables', journal: 'Am J Epidemiol', year: 1974, volume: '100', pages: '165–167', doi: '10.1093/oxfordjournals.aje.a112024' },
  felzenszwalb2012: { cite: 'Felzenszwalb PF', authors: ['Felzenszwalb PF', 'Huttenlocher DP'], title: 'Distance transforms of sampled functions', journal: 'Theory Comput', year: 2012, volume: '8', pages: '415–428', doi: '10.4086/toc.2012.v008a019' },
  foroosh2002: { cite: 'Foroosh H', authors: ['Foroosh H', 'Zerubia J', 'Berthod M'], title: 'Extension of phase correlation to subpixel registration', journal: 'IEEE Trans Image Process', year: 2002, volume: '11', pages: '188–200', doi: '10.1109/83.988953' },
  galon2006: { cite: 'Galon J', authors: ['Galon J', 'Costes A', 'Sanchez-Cabo F', 'et al.'], title: 'Type, density, and location of immune cells within human colorectal tumors predict clinical outcome', journal: 'Science', year: 2006, volume: '313', pages: '1960–1964', doi: '10.1126/science.1129139' },
  garwood1936: { cite: 'Garwood F', authors: ['Garwood F'], title: 'Fiducial limits for the Poisson distribution', journal: 'Biometrika', year: 1936, volume: '28', pages: '437–442', doi: '10.1093/biomet/28.3-4.437' },
  goltsev2018: { cite: 'Goltsev Y', authors: ['Goltsev Y', 'Samusik N', 'Kennedy-Darling J', 'et al.'], title: 'Deep profiling of mouse splenic architecture with CODEX multiplexed imaging', journal: 'Cell', year: 2018, volume: '174', pages: '968–981', doi: '10.1016/j.cell.2018.07.010' },
  hartman2024: { cite: 'Hartman A', type: 'preprint', authors: ['Hartman A', 'Satija R'], title: 'Comparative analysis of multiplexed in situ gene expression profiling technologies', journal: 'bioRxiv', year: 2024, doi: '10.1101/2024.01.11.575135' },
  hooke1961: { cite: 'Hooke R', authors: ['Hooke R', 'Jeeves TA'], title: '"Direct search" solution of numerical and statistical problems', journal: 'J ACM', year: 1961, volume: '8', pages: '212–229', doi: '10.1145/321062.321069' },
  hubert1985: { cite: 'Hubert L', authors: ['Hubert L', 'Arabie P'], title: 'Comparing partitions', journal: 'J Classif', year: 1985, volume: '2', pages: '193–218', doi: '10.1007/BF01908075' },
  iglewicz1993: { cite: 'Iglewicz B', type: 'book', authors: ['Iglewicz B', 'Hoaglin DC'], title: 'How to Detect and Handle Outliers', publisher: 'ASQC Quality Press', year: 1993 },
  janesick2023: { cite: 'Janesick A', authors: ['Janesick A', 'Shelansky R', 'Gottscho AD', 'et al.'], title: 'High resolution mapping of the tumor microenvironment using integrated single-cell, spatial and in situ analysis', journal: 'Nat Commun', year: 2023, volume: '14', pages: '8353', doi: '10.1038/s41467-023-43458-x' },
  jones2025: { cite: 'Jones DC', authors: ['Jones DC', 'Elz AE', 'Hadadianpour A', 'Ryu H', 'Glass DR', 'Newell EW'], title: 'Cell simulation as cell segmentation', journal: 'Nat Methods', year: 2025, volume: '22', pages: '1331–1342', doi: '10.1038/s41592-025-02697-0' },
  koopman1984: { cite: 'Koopman PAR', authors: ['Koopman PAR'], title: 'Confidence intervals for the ratio of two binomial proportions', journal: 'Biometrics', year: 1984, volume: '40', pages: '513–517', doi: '10.2307/2531405' },
  kumar2023: { cite: 'Kumar T', authors: ['Kumar T', 'Nee K', 'Wei R', 'et al.'], title: 'A spatially resolved single-cell genomic atlas of the adult human breast', journal: 'Nature', year: 2023, volume: '620', pages: '181–191', doi: '10.1038/s41586-023-06252-9' },
  kuglin1975: { cite: 'Kuglin CD', type: 'inproceedings', authors: ['Kuglin CD', 'Hines DC'], title: 'The phase correlation image alignment method', journal: 'Proceedings of the IEEE International Conference on Cybernetics and Society', year: 1975, pages: '163–165' },
  law2014: { cite: 'Law CW', authors: ['Law CW', 'Chen Y', 'Shi W', 'Smyth GK'], title: 'voom: precision weights unlock linear model analysis tools for RNA-seq read counts', journal: 'Genome Biol', year: 2014, volume: '15', pages: 'R29', doi: '10.1186/gb-2014-15-2-r29' },
  lee1980: { cite: 'Lee DT', authors: ['Lee DT', 'Schachter BJ'], title: 'Two algorithms for constructing a Delaunay triangulation', journal: 'Int J Comput Inf Sci', year: 1980, volume: '9', pages: '219–242', doi: '10.1007/BF00977785' },
  love2014: { cite: 'Love MI', authors: ['Love MI', 'Huber W', 'Anders S'], title: 'Moderated estimation of fold change and dispersion for RNA-seq data with DESeq2', journal: 'Genome Biol', year: 2014, volume: '15', pages: '550', doi: '10.1186/s13059-014-0550-8' },
  mann1947: { cite: 'Mann HB', authors: ['Mann HB', 'Whitney DR'], title: 'On a test of whether one of two random variables is stochastically larger than the other', journal: 'Ann Math Stat', year: 1947, volume: '18', pages: '50–60', doi: '10.1214/aoms/1177730491' },
  marconato2025: { cite: 'Marconato L', authors: ['Marconato L', 'Palla G', 'Yamauchi KA', 'et al.'], title: 'SpatialData: an open and universal data framework for spatial omics', journal: 'Nat Methods', year: 2025, volume: '22', pages: '58–62', doi: '10.1038/s41592-024-02212-x' },
  mccullagh1989: { cite: 'McCullagh P', type: 'book', authors: ['McCullagh P', 'Nelder JA'], title: 'Generalized Linear Models', edition: '2nd', publisher: 'Chapman and Hall', year: 1989 },
  mcinnes2018: { cite: 'McInnes L', type: 'preprint', authors: ['McInnes L', 'Healy J', 'Melville J'], title: 'UMAP: uniform manifold approximation and projection for dimension reduction', journal: 'arXiv', year: 2018, arxiv: '1802.03426' },
  moran1950: { cite: 'Moran PAP', authors: ['Moran PAP'], title: 'Notes on continuous stochastic phenomena', journal: 'Biometrika', year: 1950, volume: '37', pages: '17–23', doi: '10.1093/biomet/37.1-2.17' },
  okabe2000: { cite: 'Okabe A', type: 'book', authors: ['Okabe A', 'Boots B', 'Sugihara K', 'Chiu SN'], title: 'Spatial Tessellations: Concepts and Applications of Voronoi Diagrams', edition: '2nd', publisher: 'Wiley', year: 2000, doi: '10.1002/9780470317013' },
  palla2022: { cite: 'Palla G', authors: ['Palla G', 'Spitzer H', 'Klein M', 'et al.'], title: 'Squidpy: a scalable framework for spatial omics analysis', journal: 'Nat Methods', year: 2022, volume: '19', pages: '171–178', doi: '10.1038/s41592-021-01358-2' },
  reddy1996: { cite: 'Reddy BS', authors: ['Reddy BS', 'Chatterji BN'], title: 'An FFT-based technique for translation, rotation, and scale-invariant image registration', journal: 'IEEE Trans Image Process', year: 1996, volume: '5', pages: '1266–1271', doi: '10.1109/83.506761' },
  reichardt2006: { cite: 'Reichardt J', authors: ['Reichardt J', 'Bornholdt S'], title: 'Statistical mechanics of community detection', journal: 'Phys Rev E', year: 2006, volume: '74', pages: '016110', doi: '10.1103/PhysRevE.74.016110' },
  ripley1977: { cite: 'Ripley BD', authors: ['Ripley BD'], title: 'Modelling spatial patterns', journal: 'J R Stat Soc B', year: 1977, volume: '39', pages: '172–212', doi: '10.1111/j.2517-6161.1977.tb01615.x' },
  robinson2010: { cite: 'Robinson MD', authors: ['Robinson MD', 'Oshlack A'], title: 'A scaling normalization method for differential expression analysis of RNA-seq data', journal: 'Genome Biol', year: 2010, volume: '11', pages: 'R25', doi: '10.1186/gb-2010-11-3-r25' },
  rueckert1999: { cite: 'Rueckert D', authors: ['Rueckert D', 'Sonoda LI', 'Hayes C', 'Hill DLG', 'Leach MO', 'Hawkes DJ'], title: 'Nonrigid registration using free-form deformations: application to breast MR images', journal: 'IEEE Trans Med Imaging', year: 1999, volume: '18', pages: '712–721', doi: '10.1109/42.796284' },
  ruifrok2001: { cite: 'Ruifrok AC', authors: ['Ruifrok AC', 'Johnston DA'], title: 'Quantification of histochemical staining by color deconvolution', journal: 'Anal Quant Cytol Histol', year: 2001, volume: '23', pages: '291–299' },
  schapiro2017: { cite: 'Schapiro D', authors: ['Schapiro D', 'Jackson HW', 'Raghuraman S', 'et al.'], title: 'histoCAT: analysis of cell phenotypes and interactions in multiplex image cytometry data', journal: 'Nat Methods', year: 2017, volume: '14', pages: '873–876', doi: '10.1038/nmeth.4391' },
  schurch2020: { cite: 'Schürch CM', authors: ['Schürch CM', 'Bhate SS', 'Barlow GL', 'et al.'], title: 'Coordinated cellular neighborhoods orchestrate antitumoral immunity at the colorectal cancer invasive front', journal: 'Cell', year: 2020, volume: '182', pages: '1341–1359', doi: '10.1016/j.cell.2020.07.005' },
  shewchuk1997: { cite: 'Shewchuk JR', authors: ['Shewchuk JR'], title: 'Adaptive precision floating-point arithmetic and fast robust geometric predicates', journal: 'Discrete Comput Geom', year: 1997, volume: '18', pages: '305–363', doi: '10.1007/PL00009321' },
  sikkema2023: { cite: 'Sikkema L', authors: ['Sikkema L', 'Ramírez-Suástegui C', 'Strobl DC', 'et al.'], title: 'An integrated cell atlas of the lung in health and disease', journal: 'Nat Med', year: 2023, volume: '29', pages: '1563–1577', doi: '10.1038/s41591-023-02327-2' },
  smyth2004: { cite: 'Smyth GK', authors: ['Smyth GK'], title: 'Linear models and empirical Bayes methods for assessing differential expression in microarray experiments', journal: 'Stat Appl Genet Mol Biol', year: 2004, volume: '3', pages: '3', doi: '10.2202/1544-6115.1027' },
  squair2021: { cite: 'Squair JW', authors: ['Squair JW', 'Gautier M', 'Kathe C', 'et al.'], title: 'Confronting false discoveries in single-cell differential expression', journal: 'Nat Commun', year: 2021, volume: '12', pages: '5692', doi: '10.1038/s41467-021-25960-2' },
  stone1974: { cite: 'Stone M', authors: ['Stone M'], title: 'Cross-validatory choice and assessment of statistical predictions', journal: 'J R Stat Soc B', year: 1974, volume: '36', pages: '111–147', doi: '10.1111/j.2517-6161.1974.tb00994.x' },
  studholme1999: { cite: 'Studholme C', authors: ['Studholme C', 'Hill DLG', 'Hawkes DJ'], title: 'An overlap invariant entropy measure of 3D medical image alignment', journal: 'Pattern Recognit', year: 1999, volume: '32', pages: '71–86', doi: '10.1016/S0031-3203(98)00091-0' },
  tirosh2016: { cite: 'Tirosh I', authors: ['Tirosh I', 'Izar B', 'Prakadan SM', 'et al.'], title: 'Dissecting the multicellular ecosystem of metastatic melanoma by single-cell RNA-seq', journal: 'Science', year: 2016, volume: '352', pages: '189–196', doi: '10.1126/science.aad0501' },
  traag2019: { cite: 'Traag VA', authors: ['Traag VA', 'Waltman L', 'van Eck NJ'], title: 'From Louvain to Leiden: guaranteeing well-connected communities', journal: 'Sci Rep', year: 2019, volume: '9', pages: '5233', doi: '10.1038/s41598-019-41695-z' },
  travaglini2020: { cite: 'Travaglini KJ', authors: ['Travaglini KJ', 'Nabhan AN', 'Penland L', 'et al.'], title: 'A molecular cell atlas of the human lung from single-cell RNA sequencing', journal: 'Nature', year: 2020, volume: '587', pages: '619–625', doi: '10.1038/s41586-020-2922-4' },
  umeyama1991: { cite: 'Umeyama S', authors: ['Umeyama S'], title: 'Least-squares estimation of transformation parameters between two point patterns', journal: 'IEEE Trans Pattern Anal Mach Intell', year: 1991, volume: '13', pages: '376–380', doi: '10.1109/34.88573' },
  venna2001: { cite: 'Venna J, Kaski S', type: 'inproceedings', authors: ['Venna J', 'Kaski S'], title: 'Neighborhood preservation in nonlinear projection methods: an experimental study', journal: 'Artificial Neural Networks—ICANN 2001', year: 2001, pages: '485–491', doi: '10.1007/3-540-44668-0_68' },
  venna2010: { cite: 'Venna J, Peltonen J', authors: ['Venna J', 'Peltonen J', 'Nybo K', 'Aidos H', 'Kaski S'], title: 'Information retrieval perspective to nonlinear dimensionality reduction for data visualization', journal: 'J Mach Learn Res', year: 2010, volume: '11', pages: '451–490' },
  watson1981: { cite: 'Watson DF', authors: ['Watson DF'], title: 'Computing the n-dimensional Delaunay tessellation with application to Voronoi polytopes', journal: 'Comput J', year: 1981, volume: '24', pages: '167–172', doi: '10.1093/comjnl/24.2.167' },
  wedderburn1974: { cite: 'Wedderburn RWM', authors: ['Wedderburn RWM'], title: 'Quasi-likelihood functions, generalized linear models, and the Gauss–Newton method', journal: 'Biometrika', year: 1974, volume: '61', pages: '439–447', doi: '10.1093/biomet/61.3.439' },
  wilson1927: { cite: 'Wilson EB', authors: ['Wilson EB'], title: 'Probable inference, the law of succession, and statistical inference', journal: 'J Am Stat Assoc', year: 1927, volume: '22', pages: '209–212', doi: '10.1080/01621459.1927.10502953' },
  wolf2018: { cite: 'Wolf FA', authors: ['Wolf FA', 'Angerer P', 'Theis FJ'], title: 'SCANPY: large-scale single-cell gene expression data analysis', journal: 'Genome Biol', year: 2018, volume: '19', pages: '15', doi: '10.1186/s13059-017-1382-0' },
  wu2021: { cite: 'Wu SZ', authors: ['Wu SZ', 'Al-Eryani G', 'Roden DL', 'et al.'], title: 'A single-cell and spatially resolved atlas of human breast cancers', journal: 'Nat Genet', year: 2021, volume: '53', pages: '1334–1347', doi: '10.1038/s41588-021-00911-1' },
  // Software and documentation.
  xoa: { cite: '10x Genomics. Xenium Onboard Analysis', type: 'manual', authors: ['10x Genomics'], title: 'Xenium Onboard Analysis: output files and secondary analysis', year: 2024, url: 'https://www.10xgenomics.com/support/software/xenium-onboard-analysis' },
  explorer: { cite: '10x Genomics. Xenium Explorer', type: 'manual', authors: ['10x Genomics'], title: 'Xenium Explorer: image alignment', year: 2024, url: 'https://www.10xgenomics.com/support/software/xenium-explorer' },
};

// SituCraft itself, at a version.
export function situcraftReference(version) {
  return { key: 'situcraft', type: 'software', authors: ['McDermott R'], title: 'SituCraft', version, year: null, url: 'https://github.com/robert-mcdermott/situcraft' };
}

const fold = (s) => String(s ?? '').normalize('NFKD').replace(/[̀-ͯ]/g, '');

// The registry's references a citation text names. Citations are separated by "; " (or by ". "
// before the next author's name); each names the reference whose first author, as cited, it starts
// with (after a note such as "distances: ") and whose year it gives. Returns the keys in the order
// they appear, and the citations the registry does not hold, as written.
export function citationKeys(text) {
  const keys = [];
  const unknown = [];
  const parts = String(text ?? '').split(/;\s+|\.\s+(?=\p{Lu}[\p{L}'’-]+ \p{Lu}{1,4}[,.])/u).map((p) => p.trim()).filter((p) => /\p{L}/u.test(p));
  for (const written of parts) {
    const part = fold(written);
    const found = Object.entries(REFERENCES).filter(([, r]) => {
      const cite = fold(r.cite);
      return (part.startsWith(cite) || part.includes(`: ${cite}`)) && (r.type === 'manual' || part.includes(String(r.year)));
    });
    if (!found.length) unknown.push(written);
    for (const [key] of found) if (!keys.includes(key)) keys.push(key);
  }
  return { keys, unknown };
}

// The registry's reference with a DOI, or null.
export function referenceByDoi(doi) {
  const want = String(doi ?? '').trim().toLowerCase();
  if (!want) return null;
  const found = Object.entries(REFERENCES).find(([, r]) => r.doi?.toLowerCase() === want);
  return found ? found[0] : null;
}

// A reference written in Vancouver style.
export function referenceText(r) {
  const authors = r.authors.join(', ').replace(/\.$/, '');
  if (r.type === 'software') return `${authors}. ${r.title}${r.version ? `, version ${r.version}` : ''}. ${r.url}`;
  if (r.type === 'manual') return `${authors}. ${r.title}. ${r.url}`;
  if (r.type === 'book') return `${authors}. ${r.title}${r.edition ? `, ${r.edition} ed` : ''}. ${r.publisher}, ${r.year}.`;
  if (r.type === 'preprint') return `${authors}. ${r.title}. ${r.journal} ${r.year}.${r.arxiv ? ` arXiv:${r.arxiv}.` : ''}${r.doi ? ` doi:${r.doi}` : ''}`;
  const where = r.type === 'inproceedings' ? `${r.journal} ${r.year}${r.pages ? `:${r.pages}` : ''}` : `${r.journal} ${r.year}${r.volume ? `;${r.volume}` : ''}${r.pages ? `:${r.pages}` : ''}`;
  return `${authors}. ${r.title}. ${where}.${r.doi ? ` doi:${r.doi}` : ''}`;
}

// A reference as a BibTeX entry, its key the registry's.
export function bibtexEntry(key, r) {
  const type = { book: 'book', inproceedings: 'inproceedings', preprint: 'misc', manual: 'manual', software: 'software' }[r.type] ?? 'article';
  const tex = (s) => String(s).replace(/[{}]/g, '').replace(/–/g, '--').replace(/—/g, '---').replace(/&/g, '\\&');
  const fields = [
    ['author', r.authors.filter((a) => a !== 'et al.').map((a) => {
      const m = /^(.*\S)\s+([A-Z][A-Z-]*)$/.exec(a);
      return m ? `${m[1]}, ${m[2].split('').filter((c) => c !== '-').join('. ')}.` : `{${a}}`;
    }).join(' and ') + (r.authors.includes('et al.') ? ' and others' : '')],
    ['title', `{${tex(r.title)}}`],
    [type === 'article' ? 'journal' : type === 'inproceedings' ? 'booktitle' : 'howpublished', r.journal ? tex(r.journal) : null],
    ['publisher', r.publisher ? tex(r.publisher) : null],
    ['edition', r.edition ?? null],
    ['year', r.year ? String(r.year) : null],
    ['volume', r.volume ?? null],
    ['pages', r.pages ? tex(r.pages) : null],
    ['doi', r.doi ?? null],
    ['eprint', r.arxiv ?? null],
    ['archiveprefix', r.arxiv ? 'arXiv' : null],
    ['version', r.version ?? null],
    ['url', r.url ?? null],
  ].filter(([, v]) => v !== null && v !== undefined && v !== '');
  return `@${type}{${key},\n${fields.map(([k, v]) => `  ${k} = {${String(v).replace(/^\{|\}$/g, '')}}`).join(',\n')}\n}`;
}

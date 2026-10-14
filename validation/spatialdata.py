# Writes the Lung bundle as a SpatialData Zarr store with spatialdata-io's Xenium reader, the usual
# way Xenium data reach SpatialData, for the import test that reads it
# (internal/segment/imports, TestRealSpatialDataFromSpatialdataIO): its cell label image, cell
# boundaries, transcripts with their cells, and table, without the images (they do not segment).
#
#   uv run --with 'spatialdata-io==0.7.1' --with 'spatialdata==0.8.0' python -I validation/spatialdata.py [cache folder]
import os, shutil, sys
import spatialdata_io

cache = sys.argv[1] if len(sys.argv) > 1 else os.path.join(os.path.dirname(os.path.abspath(__file__)), 'cache')
bundle = os.path.join(cache, 'xenium-lung-2fov')
out = os.path.join(cache, 'xenium-lung-2fov.spatialdata.zarr')
data = spatialdata_io.xenium(bundle, morphology_focus=False, morphology_mip=False, aligned_images=False, cells_as_circles=False, n_jobs=1)
shutil.rmtree(out, ignore_errors=True)
data.write(out)
print(out)

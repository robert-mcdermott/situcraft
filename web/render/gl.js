// WebGL2 helpers for the tissue renderer: compiling programs and making buffers and textures.

// transparent: the canvas shows what is behind it where nothing is drawn (the comparison
// workspace's overlay).
export function createContext(canvas, { transparent = false } = {}) {
  const gl = canvas.getContext('webgl2', { antialias: false, alpha: transparent, depth: false, stencil: true, premultipliedAlpha: false, preserveDrawingBuffer: false, powerPreference: 'high-performance' });
  if (!gl) throw new Error('This browser cannot draw the tissue: it has no WebGL2.');
  return gl;
}

function compile(gl, type, source) {
  const shader = gl.createShader(type);
  gl.shaderSource(shader, source);
  gl.compileShader(shader);
  if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
    const log = gl.getShaderInfoLog(shader);
    gl.deleteShader(shader);
    throw new Error(`shader: ${log}`);
  }
  return shader;
}

// A program with its uniforms' locations (program.u.name) and attributes' (program.a.name).
export function createProgram(gl, vertex, fragment) {
  const program = gl.createProgram();
  const vs = compile(gl, gl.VERTEX_SHADER, vertex);
  const fs = compile(gl, gl.FRAGMENT_SHADER, fragment);
  gl.attachShader(program, vs);
  gl.attachShader(program, fs);
  gl.linkProgram(program);
  gl.deleteShader(vs);
  gl.deleteShader(fs);
  if (!gl.getProgramParameter(program, gl.LINK_STATUS)) throw new Error(`program: ${gl.getProgramInfoLog(program)}`);
  const u = {};
  for (let i = 0; i < gl.getProgramParameter(program, gl.ACTIVE_UNIFORMS); i += 1) {
    const info = gl.getActiveUniform(program, i);
    u[info.name.replace(/\[0\]$/, '')] = gl.getUniformLocation(program, info.name);
  }
  const a = {};
  for (let i = 0; i < gl.getProgramParameter(program, gl.ACTIVE_ATTRIBUTES); i += 1) {
    const info = gl.getActiveAttrib(program, i);
    a[info.name] = gl.getAttribLocation(program, info.name);
  }
  return { program, u, a };
}

export function arrayBuffer(gl, data, usage) {
  const buffer = gl.createBuffer();
  gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
  gl.bufferData(gl.ARRAY_BUFFER, data, usage ?? gl.STATIC_DRAW);
  return buffer;
}

// A vertex array of float attributes: specs [{ location, buffer, size, stride, offset }].
export function vertexArray(gl, specs) {
  const vao = gl.createVertexArray();
  gl.bindVertexArray(vao);
  for (const s of specs) {
    if (s.location === undefined || s.location < 0) continue;
    gl.bindBuffer(gl.ARRAY_BUFFER, s.buffer);
    gl.enableVertexAttribArray(s.location);
    gl.vertexAttribPointer(s.location, s.size, gl.FLOAT, false, s.stride ?? 0, s.offset ?? 0);
  }
  gl.bindVertexArray(null);
  return vao;
}

// A texture of unsigned bytes read as integers (R8UI), sampled exactly with texelFetch.
export function byteTexture(gl, width, height, data) {
  const texture = gl.createTexture();
  gl.bindTexture(gl.TEXTURE_2D, texture);
  gl.pixelStorei(gl.UNPACK_ALIGNMENT, 1);
  gl.texImage2D(gl.TEXTURE_2D, 0, gl.R8UI, width, height, 0, gl.RED_INTEGER, gl.UNSIGNED_BYTE, data);
  nearest(gl);
  return texture;
}

// A texture of 32-bit floats (R32F).
export function floatTexture(gl, width, height, data) {
  const texture = gl.createTexture();
  gl.bindTexture(gl.TEXTURE_2D, texture);
  gl.pixelStorei(gl.UNPACK_ALIGNMENT, 4);
  gl.texImage2D(gl.TEXTURE_2D, 0, gl.R32F, width, height, 0, gl.RED, gl.FLOAT, data);
  nearest(gl);
  return texture;
}

// A two-channel half-float texture (a field of displacements), interpolated between its points and
// clamped at its edges.
export function fieldTexture(gl, width, height, data) {
  const texture = gl.createTexture();
  gl.bindTexture(gl.TEXTURE_2D, texture);
  gl.pixelStorei(gl.UNPACK_ALIGNMENT, 4);
  gl.texImage2D(gl.TEXTURE_2D, 0, gl.RG16F, width, height, 0, gl.RG, gl.FLOAT, data);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
  return texture;
}

// An RGBA8 texture.
export function colorTexture(gl, width, height, data) {
  const texture = gl.createTexture();
  gl.bindTexture(gl.TEXTURE_2D, texture);
  gl.pixelStorei(gl.UNPACK_ALIGNMENT, 4);
  gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, width, height, 0, gl.RGBA, gl.UNSIGNED_BYTE, data);
  nearest(gl);
  return texture;
}

function nearest(gl) {
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
}

export function hexColor(hex) {
  const v = parseInt(hex.slice(1), 16);
  return [((v >> 16) & 255) / 255, ((v >> 8) & 255) / 255, (v & 255) / 255];
}

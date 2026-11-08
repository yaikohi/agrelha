// @ts-check
// The seam fire: the burning divider between the game rail and the worlds.
// It takes the tint of whichever game is in view and does nothing else — the
// page is whole without it, which is what happens with no WebGL, under
// prefers-reduced-motion, and below the lg breakpoint where the seam is hidden.
(function () {
  const seam = /** @type {HTMLElement} */ (document.getElementById("seam"));
  if (!seam) return;

  const root = document.documentElement;
  const secs = /** @type {HTMLElement[]} */ (Array.from(document.querySelectorAll("[data-ember]")));
  /** @type {string | null} */
  let active = null;
  /** @type {ReturnType<typeof setTimeout> | undefined} */
  let flareTimer;

  function emberVar() {
    return active ? "--color-tint-" + active : "--color-edge";
  }

  /** @param {string | null} tint */
  function setActive(tint) {
    if (tint === active) return;
    active = tint;
    seam.style.setProperty("--ember", getComputedStyle(root).getPropertyValue(emberVar()).trim());
    buildPalette();
    seam.setAttribute("data-lit", "");
    seam.classList.add("flare");
    clearTimeout(flareTimer);
    flareTimer = setTimeout(function () { seam.classList.remove("flare"); }, 140);
  }

  if ("IntersectionObserver" in window) {
    const io = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) {
        if (e.isIntersecting) setActive(e.target.getAttribute("data-ember"));
      });
    }, { rootMargin: "-25% 0px -55% 0px", threshold: 0 });
    secs.forEach(function (s) { io.observe(s); });
  }

  /* ---- flame: WebGL. fBm body + detached flamelets ---- */
  const cv = /** @type {HTMLCanvasElement | null} */ (document.getElementById("flame"));
  /** @type {WebGLRenderingContext | null} */
  let gl = null;
  /** @type {WebGLProgram | null} */
  let prog = null;
  /** @type {Record<string, WebGLUniformLocation | null>} */
  const uni = {};
  let W = 44, H = 0;
  const dpr = Math.min(1.5, window.devicePixelRatio || 1);
  let raf = 0, t0 = 0, alive = false;
  const still = window.matchMedia && window.matchMedia("(prefers-reduced-motion:reduce)").matches;

  const VS = "attribute vec2 p;void main(){gl_Position=vec4(p,0.0,1.0);}";
  const FS = [
  "#ifdef GL_FRAGMENT_PRECISION_HIGH",
  "precision highp float;",
  "#else",
  "precision mediump float;",
  "#endif",
  "uniform vec2 uRes; uniform float uTime, uDpr;",
  "uniform vec3 uC0,uC1,uC2,uC3,uC4;",
  "float hash(vec2 p){return fract(sin(dot(p,vec2(127.1,311.7)))*43758.5453123);}",
  "float noise(vec2 p){vec2 i=floor(p),f=fract(p);vec2 u=f*f*(3.0-2.0*f);",
  " return mix(mix(hash(i),hash(i+vec2(1.0,0.0)),u.x),",
  "            mix(hash(i+vec2(0.0,1.0)),hash(i+vec2(1.0,1.0)),u.x),u.y);}",
  "float fbm(vec2 p){float v=0.0,a=0.5;for(int i=0;i<5;i++){v+=a*noise(p);p*=2.03;a*=0.5;}return v;}",

  /* flamelets: blobs that pinch off a cell, rise, shrink and die */
  "float lets(vec2 q,float wc,float tm){",
  " float acc=0.0; float cell=44.0; float base=floor(q.y/cell);",
  " for(int k=0;k<4;k++){",
  "   float row=base-float(k);",
  "   float h1=hash(vec2(row,7.3)), h2=hash(vec2(row,19.1)), h3=hash(vec2(row,41.7));",
  "   float life=fract(tm*(0.30+h1*0.26)+h2);",
  "   float by=row*cell+h3*cell*0.6+life*cell*2.6;",
  "   float bx=wc*0.5+sin(tm*1.15+h2*6.283)*(2.5+h1*7.0)*life;",
  "   float r=(5.5+h3*4.0)*(1.0-life*0.72);",
  "   float d=length(vec2(q.x-bx,(q.y-by)*0.78));",
  "   acc+=smoothstep(r,0.0,d)*(1.0-life)*(1.0-life);",
  " } return acc;}",

  "void main(){",
  " vec2 q=gl_FragCoord.xy/uDpr;",
  " float wc=uRes.x/uDpr;",
  " float dx=abs(q.x-wc*0.5)/(wc*0.5);",
  " vec2 f=vec2(q.x/wc*2.6, q.y/104.0 - uTime*0.42);",
  " float warp=fbm(f*1.7+vec2(0.0,uTime*0.19));",
  " float n=fbm(f+vec2(warp*0.8,warp*0.3));",
  " float gust=0.70+0.52*noise(vec2(q.y/170.0-uTime*0.21,3.3));",
  " float body=n*gust-dx*dx*1.62-0.19;",
  " float heat=pow(clamp(body*2.6,0.0,1.0),1.25);",
  " heat+=lets(q,wc,uTime)*0.85;",
  " vec2 sp=vec2(q.x/5.5,q.y/5.5-uTime*2.0);",
  " heat+=step(0.978,noise(sp))*(1.0-dx)*0.8;",
  " float t=clamp(heat,0.0,1.0);",
  " if(t<=0.004) discard;",
  " vec3 col=mix(uC0,uC1,smoothstep(0.0,0.26,t));",
  " col=mix(col,uC2,smoothstep(0.24,0.50,t));",
  " col=mix(col,uC3,smoothstep(0.48,0.76,t));",
  " col=mix(col,uC4,smoothstep(0.80,1.0,t));",
  " float hc=uRes.y/uDpr;",
  " float fade=smoothstep(0.0,54.0,q.y)*smoothstep(0.0,54.0,hc-q.y);",
  " gl_FragColor=vec4(col,pow(t,1.55)*fade);",
  "}"].join("\n");

  /**
   * @param {number} ty
   * @param {string} src
   * @returns {WebGLShader | null}
   */
  function sh(ty, src) {
    if (!gl) return null;
    const o = gl.createShader(ty);
    if (!o) return null;
    gl.shaderSource(o, src);
    gl.compileShader(o);
    return gl.getShaderParameter(o, gl.COMPILE_STATUS) ? o : null;
  }

  function initGL() {
    if (!cv) return false;
    try {
      gl = /** @type {WebGLRenderingContext | null} */ (
        cv.getContext("webgl", { alpha: true, premultipliedAlpha: false, antialias: false }) ||
        cv.getContext("experimental-webgl")
      );
    } catch (e) {
      gl = null;
    }
    if (!gl) {
      cv.style.display = "none";
      return false;
    }
    const v = sh(gl.VERTEX_SHADER, VS);
    const f = sh(gl.FRAGMENT_SHADER, FS);
    if (!v || !f) {
      cv.style.display = "none";
      gl = null;
      return false;
    }
    prog = gl.createProgram();
    if (!prog) {
      cv.style.display = "none";
      gl = null;
      return false;
    }
    gl.attachShader(prog, v);
    gl.attachShader(prog, f);
    gl.linkProgram(prog);
    if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) {
      cv.style.display = "none";
      gl = null;
      return false;
    }
    gl.useProgram(prog);
    const b = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, b);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 3, -1, -1, 3]), gl.STATIC_DRAW);
    const loc = gl.getAttribLocation(prog, "p");
    gl.enableVertexAttribArray(loc);
    gl.vertexAttribPointer(loc, 2, gl.FLOAT, false, 0, 0);
    ["uRes", "uTime", "uDpr", "uC0", "uC1", "uC2", "uC3", "uC4"]
      .forEach(function (n) {
        if (gl && prog) uni[n] = gl.getUniformLocation(prog, n);
      });
    gl.enable(gl.BLEND);
    gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
    return true;
  }

  /**
   * @param {string} v
   * @returns {[number, number, number]}
   */
  function hex(v) {
    v = (v || "").trim();
    if (v.charAt(0) !== "#") return [0.3, 0.25, 0.2];
    if (v.length === 4) v = "#" + v[1] + v[1] + v[2] + v[2] + v[3] + v[3];
    return [parseInt(v.substr(1, 2), 16) / 255, parseInt(v.substr(3, 2), 16) / 255, parseInt(v.substr(5, 2), 16) / 255];
  }

  /**
   * @param {[number, number, number]} a
   * @param {[number, number, number]} b
   * @param {number} t
   * @returns {[number, number, number]}
   */
  function mix3(a, b, t) {
    return [a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t, a[2] + (b[2] - a[2]) * t];
  }

  /* The flame is the same light source as the lamps, so its ramp is built from
     their tokens rather than from fire colours of its own: ember in dark,
     violet in light, and whatever a later palette makes them. The game tint
     only colours the coolest edge, so the seam still says which game you are on
     without muddying the flame.
     @returns {[number, number, number][]} coolest first, hottest last */
  function lampRamp(cs, light) {
    const out = hex(cs.getPropertyValue("--color-lamp-out"));
    const dim = hex(cs.getPropertyValue("--color-lamp-dim"));
    const lit = hex(cs.getPropertyValue("--color-lamp-lit"));
    if (light) {
      // On a cream ground the core has to go deeper to stay visible, not whiter.
      return [mix3(out, dim, 0.55), dim, mix3(dim, lit, 0.55), lit, mix3(lit, [0, 0, 0], 0.3)];
    }
    return [mix3(out, dim, 0.3), mix3(out, dim, 0.7), dim, lit, mix3(lit, [1, 1, 1], 0.55)];
  }

  function buildPalette() {
    if (!gl || !prog) return;
    const cs = getComputedStyle(root);
    const tint = hex(cs.getPropertyValue(emberVar()));
    const surf = hex(cs.getPropertyValue("--color-surface"));
    const light = (0.2126 * surf[0] + 0.7152 * surf[1] + 0.0722 * surf[2]) > 0.5;
    const R = lampRamp(cs, light);
    gl.useProgram(prog);
    gl.uniform3fv(uni.uC0, new Float32Array(mix3(R[0], tint, 0.45)));
    gl.uniform3fv(uni.uC1, new Float32Array(mix3(R[1], tint, 0.22)));
    gl.uniform3fv(uni.uC2, new Float32Array(R[2]));
    gl.uniform3fv(uni.uC3, new Float32Array(R[3]));
    gl.uniform3fv(uni.uC4, new Float32Array(R[4]));
  }

  const PAD = 64;
  function layout() {
    if (!gl || !cv || !prog) return;
    const rect = seam.getBoundingClientRect();
    W = Math.round(rect.width) || 44;
    H = Math.max(40, Math.round(rect.height)) + PAD * 2;
    cv.style.top = (-PAD) + "px";
    cv.style.height = H + "px";
    const pw = Math.round(W * dpr), ph = Math.min(8192, Math.round(H * dpr));
    cv.width = pw;
    cv.height = ph;
    gl.viewport(0, 0, pw, ph);
    gl.useProgram(prog);
    gl.uniform2f(uni.uRes, pw, ph);
    gl.uniform1f(uni.uDpr, dpr);
  }

  /** @param {number} ts */
  function render(ts) {
    if (!gl || !prog) return;
    gl.useProgram(prog);
    gl.uniform1f(uni.uTime, ts);
    gl.clearColor(0, 0, 0, 0);
    gl.clear(gl.COLOR_BUFFER_BIT);
    gl.drawArrays(gl.TRIANGLES, 0, 3);
  }

  /** @param {number} ts */
  function loop(ts) {
    raf = requestAnimationFrame(loop);
    if (!t0) t0 = ts;
    render(((ts - t0) / 1000) % 3600);
  }

  function start() {
    if (alive || still || !gl || document.hidden) return;
    alive = true;
    raf = requestAnimationFrame(loop);
  }

  function stop() {
    alive = false;
    cancelAnimationFrame(raf);
  }

  if (initGL()) {
    layout();
    buildPalette();
    render(3.0);
    if (document.fonts && document.fonts.ready) {
      document.fonts.ready.then(function () { layout(); render(3.0); });
    }
    if (!still) {
      let seamVisible = true;
      if ("IntersectionObserver" in window) {
        new IntersectionObserver(function (e) {
          seamVisible = e[0].isIntersecting;
          if (seamVisible && !document.hidden) {
            start();
          } else {
            stop();
          }
        }, { threshold: 0 }).observe(seam);
      } else {
        start();
      }

      document.addEventListener("visibilitychange", function () {
        if (document.hidden) {
          stop();
        } else if (seamVisible) {
          start();
        }
      });
    }
    if ("ResizeObserver" in window) {
      new ResizeObserver(function () { layout(); render(3.0); }).observe(seam);
    }
  }

  if ("MutationObserver" in window) {
    new MutationObserver(function () { buildPalette(); render(3.0); })
      .observe(root, { attributes: true, attributeFilter: ["data-theme"] });
  }

  if (secs.length > 0) setActive(secs[0].getAttribute("data-ember"));
})();

// WebView adapter for gofastr.capture/v1. The camera, microphone and sensors
// run in the page: WebCodecs encodes H.264 (Annex B) and Opus, and the page
// posts samples and telemetry to capture.WebViewPath, where Go runs the same
// session, recording and sinks as for the Android adapter. Capture lasts
// while the page does; it has no background service.
(() => {
  'use strict';
  const name = 'gofastr.capture/v1';
  const path = '/_capture/device';
  const flushEvery = 100; // ms between reports while capturing
  const backlogLimit = 4 << 20; // unsent bytes before frames are dropped
  // A report Go never answers would block every later one, the stop
  // directive and the stopped event included, so it counts as failed.
  const reportTimeout = 10000; // ms
  const flushTimeout = 2000; // ms an encoder gets to drain when capture stops

  const supported = () => window.isSecureContext && typeof navigator.mediaDevices?.getUserMedia === 'function' &&
    typeof window.VideoEncoder === 'function' && typeof window.MediaStreamTrackProcessor === 'function';
  const send = (input, init) => (window.__gofastr_transport || window.fetch.bind(window))(input, init);
  const clock = () => Math.round(performance.now() * 1e6);
  const round = value => Math.round(value * 100) / 100;

  // ---- reports to Go -------------------------------------------------------
  // One report in flight at a time; items keep their order across reports.
  const reporter = (id, onDirectives) => {
    let items = [], chunks = [], bytes = 0, busy = false, last = 0, closed = false;
    const encoder = new TextEncoder();
    const post = async () => {
      if (busy || closed && !items.length) return;
      busy = true;
      const batch = items, parts = chunks;
      items = []; chunks = []; bytes = 0; last = performance.now();
      try {
        const header = encoder.encode(JSON.stringify({ session: id, items: batch }));
        const body = new Uint8Array(4 + header.length + parts.reduce((sum, part) => sum + part.length, 0));
        new DataView(body.buffer).setUint32(0, header.length);
        body.set(header, 4);
        let offset = 4 + header.length;
        for (const part of parts) { body.set(part, offset); offset += part.length; }
        const response = await send(path, {
          method: 'POST', headers: { 'Content-Type': 'application/octet-stream' }, body, signal: AbortSignal.timeout?.(reportTimeout),
        });
        onDirectives(response.ok ? await response.text() : 'stop');
      } catch (_) {
        onDirectives('stop');
      } finally {
        busy = false;
      }
    };
    const timer = setInterval(() => {
      if (items.length || performance.now() - last >= 1000) post();
    }, flushEvery);
    return {
      event(kind, payload) { items.push({ event: kind, payload: JSON.stringify(payload) }); },
      sample(track, key, at, data) {
        items.push({ track, flags: key ? 1 : 0, clock: at, size: data.length });
        chunks.push(data); bytes += data.length;
      },
      get backlog() { return bytes; },
      // flush sends what is queued and waits for it.
      async flush() {
        while (busy) await new Promise(resolve => setTimeout(resolve, 10));
        await post();
      },
      close() { closed = true; clearInterval(timer); },
    };
  };

  // ---- camera direction ----------------------------------------------------
  // The W3C device orientation angles give the device-to-world (east, north,
  // up) rotation matrix, the same matrix Android's rotation vector gives, so
  // azimuth, pitch and roll match the Android adapter.
  const orientation = (event, front, at) => {
    const radians = Math.PI / 180;
    const a = (event.alpha || 0) * radians, b = (event.beta || 0) * radians, g = (event.gamma || 0) * radians;
    const cA = Math.cos(a), sA = Math.sin(a), cB = Math.cos(b), sB = Math.sin(b), cG = Math.cos(g), sG = Math.sin(g);
    const m = [
      cA * cG - sA * sB * sG, -cB * sA, cG * sA * sB + cA * sG,
      cG * sA + cA * sB * sG, cA * cB, sA * sG - cA * cG * sB,
      -cB * sG, sB, cB * cG,
    ];
    // The back camera looks along the device's -Z axis, the front along +Z.
    const sign = front ? 1 : -1;
    const east = sign * m[2], north = sign * m[5], up = sign * m[8];
    const azimuth = (Math.atan2(east, north) / radians + 360) % 360;
    const pitch = Math.asin(Math.max(-1, Math.min(1, up))) / radians;
    const roll = Math.atan2(m[6], m[7]) / radians;
    // Rotation matrix to quaternion (x, y, z, w).
    const trace = m[0] + m[4] + m[8];
    let x, y, z, w;
    if (trace > 0) {
      const s = Math.sqrt(trace + 1) * 2;
      w = s / 4; x = (m[7] - m[5]) / s; y = (m[2] - m[6]) / s; z = (m[3] - m[1]) / s;
    } else if (m[0] > m[4] && m[0] > m[8]) {
      const s = Math.sqrt(1 + m[0] - m[4] - m[8]) * 2;
      w = (m[7] - m[5]) / s; x = s / 4; y = (m[1] + m[3]) / s; z = (m[2] + m[6]) / s;
    } else if (m[4] > m[8]) {
      const s = Math.sqrt(1 + m[4] - m[0] - m[8]) * 2;
      w = (m[2] - m[6]) / s; x = (m[1] + m[3]) / s; y = s / 4; z = (m[5] + m[7]) / s;
    } else {
      const s = Math.sqrt(1 + m[8] - m[0] - m[4]) * 2;
      w = (m[3] - m[1]) / s; x = (m[2] + m[6]) / s; y = (m[5] + m[7]) / s; z = s / 4;
    }
    return { clock: at, azimuth: round(azimuth), pitch: round(pitch), roll: round(roll), quaternion: [x, y, z, w] };
  };

  // H.264 constrained baseline at a level that fits the frame size.
  const avcCodec = (width, height) => {
    const macroblocks = Math.ceil(width / 16) * Math.ceil(height / 16);
    const level = macroblocks <= 3600 ? '1f' : macroblocks <= 8192 ? '28' : '33';
    return 'avc1.42e0' + level;
  };

  // ---- the page's one camera ------------------------------------------------
  const engine = {
    stream: null, // MediaStream shown in previews
    facing: '',
    opening: null,
    views: new Set(),
    session: null,
    ended: new Set(), // session ids this page finished
    error: '',
  };
  const show = () => {
    for (const view of engine.views) {
      if (view.video.srcObject !== engine.stream) view.video.srcObject = engine.stream;
      if (engine.stream) view.video.play().catch(() => {});
      view.status.textContent = engine.error;
    }
  };
  const release = () => {
    for (const track of engine.stream?.getTracks() || []) track.stop();
    engine.stream = null; engine.facing = '';
    show();
  };
  const constraints = (camera, config) => ({
    video: {
      facingMode: camera === 'front' ? 'user' : 'environment',
      width: { ideal: config?.width || 1280 }, height: { ideal: config?.height || 720 },
      frameRate: { ideal: config?.frameRate || 30 },
    },
    audio: !!config?.audio && { channelCount: 1, echoCancellation: false, noiseSuppression: false, autoGainControl: true },
  });
  // preview opens the camera for a screen with no running session.
  const preview = async camera => {
    if (engine.session || engine.stream && engine.facing === camera) return;
    if (engine.opening) { await engine.opening; return preview(camera); }
    release();
    engine.opening = navigator.mediaDevices.getUserMedia(constraints(camera)).then(stream => {
      if (engine.session || !engine.views.size) { for (const track of stream.getTracks()) track.stop(); return; }
      engine.stream = stream; engine.facing = camera; engine.error = '';
    }, () => { engine.error = 'The camera is not available.'; }).finally(() => { engine.opening = null; show(); });
    await engine.opening;
  };

  // ---- one capture session --------------------------------------------------
  const start = async ({ id, config }) => {
    if (engine.session || engine.ended.has(id)) return;
    const front = config.camera === 'front';
    const state = { id, stopping: null, needKey: true, video: null, audio: null, readers: [], listeners: [], watch: null, offset: { video: null, audio: null } };
    engine.session = state;
    const report = reporter(id, directives => {
      for (const directive of directives.split(',')) {
        if (directive === 'stop') stop('requested');
        else if (directive === 'keyframe') state.needKey = true;
        else if (directive.startsWith('bitrate:') && state.video?.state === 'configured') {
          state.videoConfig.bitrate = Number(directive.slice(8)) || state.videoConfig.bitrate;
          state.video.configure(state.videoConfig);
          state.needKey = true;
        }
      }
    });
    state.report = report;
    const fail = (reason, message) => stop(reason, message, true);

    const stop = (reason, message, failed) => state.stopping ||= (async () => {
      for (const reader of state.readers) reader.cancel().catch(() => {});
      for (const [target, type, listener] of state.listeners) target.removeEventListener(type, listener);
      if (state.watch !== null) navigator.geolocation.clearWatch(state.watch);
      for (const encoder of [state.video, state.audio]) {
        // The stopped event waits on this, so a flush that never settles
        // must not keep the session stopping.
        if (encoder?.state === 'configured') {
          await Promise.race([encoder.flush().catch(() => {}), new Promise(resolve => setTimeout(resolve, flushTimeout))]);
        }
        if (encoder && encoder.state !== 'closed') encoder.close();
      }
      report.event(failed ? 'failed' : 'stopped', { reason, message: message || '' });
      await report.flush();
      report.close();
      engine.ended.add(id);
      if (engine.session === state) engine.session = null;
      release();
      // A screen still showing the camera goes back to its preview.
      const view = engine.views.values().next().value;
      if (view) preview(view.camera);
    })();

    let stream;
    try {
      release();
      try {
        stream = await navigator.mediaDevices.getUserMedia(constraints(config.camera, config));
      } catch (error) {
        if (!config.audio) throw error;
        stream = await navigator.mediaDevices.getUserMedia(constraints(config.camera, { ...config, audio: false }));
      }
    } catch (_) {
      engine.error = 'The camera is not available.';
      show();
      return fail('permission', 'Camera permission was denied or the camera is busy.');
    }
    if (state.stopping) { for (const track of stream.getTracks()) track.stop(); return; }
    engine.stream = stream; engine.facing = config.camera; engine.error = '';
    show();
    const [videoTrack] = stream.getVideoTracks();
    const [audioTrack] = stream.getAudioTracks();
    videoTrack.addEventListener('ended', () => fail('interrupted', 'The camera stopped.'));

    // The first frame decides the encoded size.
    const frames = typeof window.MediaStreamTrackProcessor === 'function' ?
      new MediaStreamTrackProcessor({ track: videoTrack }).readable.getReader() : null;
    if (!frames) return fail('unsupported', 'This WebView cannot read camera frames.');
    state.readers.push(frames);
    const first = await frames.read();
    if (first.done || state.stopping) { first.value?.close(); return; }
    const width = first.value.displayWidth & ~1, height = first.value.displayHeight & ~1;
    state.videoConfig = {
      codec: avcCodec(width, height), width, height, bitrate: config.bitrate, framerate: config.frameRate,
      latencyMode: 'realtime', bitrateMode: 'variable', avc: { format: 'annexb' },
    };
    const videoSupport = await VideoEncoder.isConfigSupported(state.videoConfig).catch(() => null);
    if (!videoSupport?.supported) {
      first.value.close();
      return fail('unsupported', 'This WebView cannot encode H.264 video.');
    }
    let frameCount = 0;
    state.video = new VideoEncoder({
      output: (chunk) => {
        const data = new Uint8Array(chunk.byteLength);
        chunk.copyTo(data);
        report.sample(0, chunk.type === 'key', Math.round((chunk.timestamp + state.offset.video) * 1000), data);
      },
      error: error => fail('encoder', String(error?.message || error)),
    });
    state.video.configure(state.videoConfig);

    let audioActive = false;
    if (audioTrack && typeof window.AudioEncoder === 'function') {
      const support = await AudioEncoder.isConfigSupported({ codec: 'opus', sampleRate: 48000, numberOfChannels: 1 }).catch(() => null);
      audioActive = !!support?.supported;
    }
    if (!audioActive && audioTrack) audioTrack.stop();

    const motion = (config.motion || config.gyroscope) && typeof window.DeviceOrientationEvent === 'function';
    const acceleration = !!config.acceleration && typeof window.DeviceMotionEvent === 'function';
    let location = config.location && !!navigator.geolocation;
    if (location && navigator.permissions?.query) {
      const status = await navigator.permissions.query({ name: 'geolocation' }).catch(() => null);
      if (status?.state === 'denied') location = false;
    }
    const startClock = clock();
    report.event('started', {
      width, height, rotation: 0, audio: audioActive, location, motion, acceleration,
      clock: startClock, wall: new Date(performance.timeOrigin + startClock / 1e6).toISOString(), recorder: 'go',
    });

    // Video: encode every frame the encoder keeps up with.
    const keyInterval = 2 * config.frameRate;
    const encode = frame => {
      if (state.stopping || state.video.state !== 'configured') { frame.close(); return; }
      state.offset.video ??= performance.now() * 1000 - frame.timestamp;
      if (state.video.encodeQueueSize > 2 || report.backlog > backlogLimit) {
        frame.close(); state.needKey = true; return;
      }
      const key = state.needKey || frameCount % keyInterval === 0;
      state.needKey = false;
      frameCount++;
      state.video.encode(frame, { keyFrame: key });
      frame.close();
    };
    encode(first.value);
    (async () => {
      for (;;) {
        const { value, done } = await frames.read().catch(() => ({ done: true }));
        if (done) return;
        encode(value);
      }
    })();

    // Audio: Opus from the microphone, configured from the first buffer.
    if (audioActive) {
      const samples = new MediaStreamTrackProcessor({ track: audioTrack }).readable.getReader();
      state.readers.push(samples);
      (async () => {
        for (;;) {
          const { value, done } = await samples.read().catch(() => ({ done: true }));
          if (done) return;
          if (state.stopping) { value.close(); continue; }
          if (!state.audio) {
            state.audio = new AudioEncoder({
              output: chunk => {
                const data = new Uint8Array(chunk.byteLength);
                chunk.copyTo(data);
                report.sample(1, true, Math.round((chunk.timestamp + state.offset.audio) * 1000), data);
              },
              error: () => { if (state.audio?.state !== 'closed') state.audio.close(); },
            });
            state.audio.configure({ codec: 'opus', sampleRate: value.sampleRate, numberOfChannels: value.numberOfChannels, bitrate: 64000 });
          }
          state.offset.audio ??= performance.now() * 1000 - value.timestamp;
          if (state.audio.state === 'configured') state.audio.encode(value);
          value.close();
        }
      })();
    }

    // Location: one fix per update, on the page clock.
    if (location) {
      state.watch = navigator.geolocation.watchPosition(position => {
        if (state.stopping) return;
        const c = position.coords;
        const fix = {
          clock: Math.round((position.timestamp - performance.timeOrigin) * 1e6),
          time: new Date(position.timestamp).toISOString(), provider: 'webview',
          latitude: c.latitude, longitude: c.longitude, accuracy: c.accuracy ?? -1,
        };
        if (c.altitude != null) fix.altitude = c.altitude;
        if (c.altitudeAccuracy != null) fix.verticalAccuracy = c.altitudeAccuracy;
        if (c.speed != null && !Number.isNaN(c.speed)) fix.speed = c.speed;
        if (c.heading != null && !Number.isNaN(c.heading)) fix.bearing = c.heading;
        report.event('location', fix);
      }, () => {}, { enableHighAccuracy: true, maximumAge: 0 });
    }

    // Motion: camera direction and, when asked, the gyroscope and
    // acceleration.
    const interval = 1000 / (config.motionRate || 30);
    const listen = (target, type, listener) => { target.addEventListener(type, listener); state.listeners.push([target, type, listener]); };
    if (motion) {
      let lastOrientation = 0, lastRotation = 0;
      const absolute = 'ondeviceorientationabsolute' in window;
      if (config.motion) {
        listen(window, absolute ? 'deviceorientationabsolute' : 'deviceorientation', event => {
          if (state.stopping || event.alpha == null || event.timeStamp - lastOrientation < interval) return;
          lastOrientation = event.timeStamp;
          report.event('orientation', orientation(event, front, Math.round(event.timeStamp * 1e6)));
        });
      }
      if (config.gyroscope) {
        listen(window, 'devicemotion', event => {
          const rate = event.rotationRate;
          if (state.stopping || rate?.alpha == null || event.timeStamp - lastRotation < interval) return;
          lastRotation = event.timeStamp;
          const radians = Math.PI / 180;
          report.event('rotation', { clock: Math.round(event.timeStamp * 1e6), x: rate.beta * radians, y: rate.gamma * radians, z: rate.alpha * radians });
        });
      }
    }
    if (acceleration) {
      // Without event.acceleration, a low-pass filter estimates gravity to
      // remove from accelerationIncludingGravity, as the Android adapter does.
      let lastAcceleration = 0, gravity = null;
      listen(window, 'devicemotion', event => {
        let a = event.acceleration;
        if (a?.x == null) {
          const g = event.accelerationIncludingGravity;
          if (g?.x == null) return;
          gravity = gravity ? gravity.map((value, i) => 0.9 * value + 0.1 * [g.x, g.y, g.z][i]) : [g.x, g.y, g.z];
          a = { x: g.x - gravity[0], y: g.y - gravity[1], z: g.z - gravity[2] };
        }
        if (state.stopping || event.timeStamp - lastAcceleration < interval) return;
        lastAcceleration = event.timeStamp;
        report.event('acceleration', { clock: Math.round(event.timeStamp * 1e6), x: round(a.x), y: round(a.y), z: round(a.z) });
      });
    }
  };

  // ---- the component --------------------------------------------------------
  const parse = component => {
    if (component?.name !== name || typeof component.payload !== 'string') throw new Error('Invalid capture component.');
    const view = JSON.parse(component.payload);
    if (!view || typeof view !== 'object' || !['back', 'front'].includes(view.camera)) throw new Error('Invalid capture payload.');
    if (view.session && (!/^[0-9a-f]{32}$/.test(view.session.id) || typeof view.session.config !== 'object')) throw new Error('Invalid capture session.');
    return view;
  };

  const mount = (container, component) => {
    const video = document.createElement('video');
    video.muted = true; video.playsInline = true; video.autoplay = true;
    video.setAttribute('aria-label', 'Camera preview');
    Object.assign(video.style, { display: 'block', width: '100%', aspectRatio: '3 / 4', objectFit: 'cover', background: '#000', borderRadius: '.75rem' });
    const status = document.createElement('p');
    status.setAttribute('role', 'status');
    Object.assign(status.style, { margin: '.5rem 0 0', color: 'var(--color-text-muted)', fontSize: '.875rem' });
    container.setAttribute('data-gfw-capture', '');
    container.replaceChildren(video, status);
    const view = { video, status, camera: 'back' };
    engine.views.add(view);
    const update = next => {
      const wire = parse(next);
      view.camera = wire.camera;
      if (wire.session && !engine.ended.has(wire.session.id)) start(wire.session);
      else if (wire.preview) preview(wire.camera);
      show();
    };
    update(component);
    return {
      update,
      dispose() {
        engine.views.delete(view);
        video.srcObject = null;
        container.replaceChildren();
        container.removeAttribute('data-gfw-capture');
        if (!engine.views.size && !engine.session) release();
      },
    };
  };

  (window.__gofastr_component_extension_definitions ||= []).push({
    name,
    capabilities: () => ({ [name]: supported() ? 'available' : 'unavailable' }),
    mount,
  });
})();

(() => {
  'use strict';
  // The instrument is drawn by the Android and UIKit renderers only; the
  // WebView renderer shows a note in its place.
  const name = 'dev.gofastr.chordharp/v1';
  const mount = container => {
    const note = document.createElement('p');
    note.textContent = 'The chord harp plays in the Android and iOS apps. The WebView version is not written yet.';
    container.append(note);
    return { update() {}, dispose() { note.remove(); } };
  };
  (window.__gofastr_component_extension_definitions ||= []).push({ name, capabilities: () => ({ [name]: 'available' }), mount });
})();

(function () {
  var el = document.getElementById("events");
  if (!el) return;

  var id = el.dataset.session;
  var es = new EventSource("/api/sessions/" + id + "/events");

  function add(ev) {
    var div = document.createElement("div");
    var detail = ev.message || ev.result || ev.args || ev.status || "";
    if (ev.tool) detail = "[" + ev.tool + "] " + detail;
    div.textContent = ev.type + (detail ? ": " + detail : "");
    el.appendChild(div);
    if (ev.type === "session.done" || ev.type === "session.failed") es.close();
  }

  es.onmessage = function (e) {
    try {
      add(JSON.parse(e.data));
    } catch (_) {}
  };
})();

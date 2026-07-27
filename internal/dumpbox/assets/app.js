const drop = document.querySelector("#drop");
const picker = document.querySelector("#picker");
const queue = document.querySelector("#queue");
["dragenter", "dragover"].forEach(name => drop.addEventListener(name, event => {
  event.preventDefault(); drop.classList.add("dragging");
}));
["dragleave", "drop"].forEach(name => drop.addEventListener(name, event => {
  event.preventDefault(); drop.classList.remove("dragging");
}));
drop.addEventListener("drop", event => uploadAll(event.dataTransfer.files));
picker.addEventListener("change", () => { uploadAll(picker.files); picker.value = ""; });
function uploadAll(files) { Array.from(files).forEach(upload); }
function upload(file) {
  const item = document.createElement("div");
  item.className = "file";
  const row = document.createElement("div"); row.className = "row";
  const filename = document.createElement("div"); filename.className = "filename"; filename.textContent = file.name;
  const result = document.createElement("div"); result.className = "result";
  const status = document.createElement("div"); status.className = "status";
  const retry = document.createElement("button"); retry.className = "retry"; retry.type = "button"; retry.textContent = "Retry"; retry.hidden = true;
  const bar = document.createElement("div"); bar.className = "bar";
  const fill = document.createElement("div"); fill.className = "fill";
  result.append(status, retry); row.append(filename, result); bar.append(fill); item.append(row, bar); queue.append(item);

  function fail(message) {
    item.classList.add("failed"); status.textContent = message; retry.hidden = false;
  }
  function send() {
    item.classList.remove("done", "failed"); fill.style.width = "0"; status.textContent = "Starting…"; retry.hidden = true;
    const body = new FormData(); body.append("file", file);
    const request = new XMLHttpRequest(); request.open("POST", "upload"); request.setRequestHeader("X-Dumpbox-Upload", "1");
    request.upload.onprogress = event => {
      if (!event.lengthComputable) return;
      const percent = Math.round(event.loaded / event.total * 100);
      fill.style.width = percent + "%"; status.textContent = percent + "%";
    };
    request.onload = () => {
      let response = {}; try { response = JSON.parse(request.responseText); } catch (_) {}
      if (request.status === 201) {
        item.classList.add("done"); fill.style.width = "100%"; status.textContent = "Uploaded";
      } else {
        fail(response.error || "Upload failed");
      }
    };
    request.onerror = () => fail("Connection lost");
    request.send(body);
  }
  retry.addEventListener("click", send);
  send();
}

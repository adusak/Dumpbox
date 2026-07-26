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
  const status = document.createElement("div"); status.className = "status"; status.textContent = "Starting…";
  const bar = document.createElement("div"); bar.className = "bar";
  const fill = document.createElement("div"); fill.className = "fill";
  row.append(filename, status); bar.append(fill); item.append(row, bar); queue.prepend(item);
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
      item.classList.add("failed"); status.textContent = response.error || "Upload failed";
    }
  };
  request.onerror = () => { item.classList.add("failed"); status.textContent = "Connection lost"; };
  request.send(body);
}

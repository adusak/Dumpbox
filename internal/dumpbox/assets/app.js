const drop = document.querySelector("#drop");
const picker = document.querySelector("#picker");
const queue = document.querySelector("#queue");
["dragenter", "dragover"].forEach(name => drop.addEventListener(name, event => {
  event.preventDefault(); drop.classList.add("dragging");
}));
["dragleave", "drop"].forEach(name => drop.addEventListener(name, event => {
  event.preventDefault(); drop.classList.remove("dragging");
}));
drop.addEventListener("drop", async event => {
  const entries = await droppedEntries(event.dataTransfer);
  entries.forEach(entry => entry.root ? upload(entry.files, entry.root) : upload(entry.file));
});
picker.addEventListener("change", () => { uploadAll(picker.files); picker.value = ""; });
function uploadAll(files) { Array.from(files).forEach(upload); }
async function droppedEntries(dataTransfer) {
  const entries = Array.from(dataTransfer.items || [])
    .map(item => typeof item.webkitGetAsEntry === "function" ? item.webkitGetAsEntry() : null)
    .filter(Boolean);
  if (entries.length === 0) return Array.from(dataTransfer.files, file => ({ file }));
  return Promise.all(entries.map(async entry => {
    if (entry.isFile) {
      const files = await filesFromEntry(entry, "");
      return { file: files[0].file };
    }
    return { root: entry.name, files: await filesFromDirectory(entry) };
  }));
}
function filesFromDirectory(entry) {
  return readDirectory(entry).then(entries => Promise.all(entries.map(child => filesFromEntry(child, ""))))
    .then(files => files.flat());
}
function filesFromEntry(entry, path) {
  if (entry.isFile) {
    return new Promise((resolve, reject) => entry.file(file => resolve([{ file, path }]), reject));
  }
  if (!entry.isDirectory) return Promise.resolve([]);
  const childPath = path ? path + "/" + entry.name : entry.name;
  return readDirectory(entry).then(entries => Promise.all(entries.map(child => filesFromEntry(child, childPath))))
    .then(files => files.flat());
}
function readDirectory(entry) {
  const reader = entry.createReader();
  const entries = [];
  return new Promise((resolve, reject) => {
    function read() {
      reader.readEntries(batch => {
        if (batch.length === 0) {
          resolve(entries);
          return;
        }
        entries.push(...batch);
        read();
      }, reject);
    }
    read();
  });
}
function upload(value, root = "") {
  const files = root ? value : [{ file: value, path: "" }];
  const item = document.createElement("div");
  item.className = "file";
  const row = document.createElement("div"); row.className = "row";
  const filename = document.createElement("div"); filename.className = "filename"; filename.textContent = root || value.name;
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
    const body = new FormData();
    if (root) body.append("root", root);
    files.forEach(entry => {
      if (root) body.append("path", entry.path);
      body.append("file", entry.file);
    });
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

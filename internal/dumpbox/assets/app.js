const drop = document.querySelector("#drop");
const picker = document.querySelector("#picker");
const folderPicker = document.querySelector("#folder-picker");
const queue = document.querySelector("#queue");
["dragenter", "dragover"].forEach(name => drop.addEventListener(name, event => {
  event.preventDefault(); drop.classList.add("dragging");
}));
["dragleave", "drop"].forEach(name => drop.addEventListener(name, event => {
  event.preventDefault(); drop.classList.remove("dragging");
}));
drop.addEventListener("drop", async event => {
  try {
    const entries = await droppedEntries(event.dataTransfer);
    if (entries.length === 0) {
      reportDropError();
      return;
    }
    uploadEntries(entries);
  } catch (_) {
    reportDropError();
  }
});
picker.addEventListener("change", () => { uploadAll(picker.files); picker.value = ""; });
folderPicker.addEventListener("change", () => {
  uploadEntries(entriesFromFiles(folderPicker.files));
  folderPicker.value = "";
});
function uploadAll(files) { Array.from(files).forEach(upload); }
function uploadEntries(entries) {
  entries.forEach(entry => entry.root ? upload(entry.files, entry.root) : upload(entry.file));
}
function entriesFromFiles(files) {
  const entries = [];
  const folders = new Map();
  Array.from(files || []).forEach(file => {
    const parts = (file.webkitRelativePath || "").split("/").filter(Boolean);
    if (parts.length < 2) {
      entries.push({ file });
      return;
    }
    const root = parts.shift();
    parts.pop();
    let folder = folders.get(root);
    if (!folder) {
      folder = { root, files: [] };
      folders.set(root, folder);
      entries.push(folder);
    }
    folder.files.push({ file, path: parts.join("/") });
  });
  return entries;
}
async function droppedEntries(dataTransfer) {
  const items = Array.from(dataTransfer.items || []);
  const transferred = entriesFromFiles(dataTransfer.files);
  const dropped = await Promise.all(items.map(async item => {
    const getEntry = typeof item.getAsEntry === "function" ? item.getAsEntry : item.webkitGetAsEntry;
    const entry = typeof getEntry === "function" ? getEntry.call(item) : null;
    if (!entry) {
      const file = typeof item.getAsFile === "function" ? item.getAsFile() : null;
      return file ? { file } : null;
    }
    if (entry.isFile) {
      const files = await filesFromEntry(entry, "");
      return files.length > 0 ? { file: files[0].file } : null;
    }
    return { root: entry.name, files: await filesFromDirectory(entry) };
  }));
  const usable = dropped.filter(Boolean);
  return transferred.some(entry => entry.root) ? transferred : (usable.length > 0 ? usable : transferred);
}
function filesFromDirectory(entry) {
  return readDirectory(entry).then(entries => Promise.all(entries.map(child => filesFromEntry(child, ""))))
    .then(groups => groups.reduce((files, group) => files.concat(group), []));
}
function filesFromEntry(entry, path) {
  if (entry.isFile) {
    return new Promise((resolve, reject) => entry.file(file => resolve([{ file, path }]), reject));
  }
  if (!entry.isDirectory) return Promise.resolve([]);
  const childPath = path ? path + "/" + entry.name : entry.name;
  return readDirectory(entry).then(entries => Promise.all(entries.map(child => filesFromEntry(child, childPath))))
    .then(groups => groups.reduce((files, group) => files.concat(group), []));
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
function reportDropError() {
  const item = document.createElement("div");
  item.className = "file failed";
  const status = document.createElement("div");
  status.className = "status";
  status.textContent = "This browser could not read that folder. Use Choose folder instead.";
  item.append(status);
  queue.append(item);
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

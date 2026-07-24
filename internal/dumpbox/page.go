package dumpbox

const landingHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Dumpbox</title>
  <link rel="icon" type="image/svg+xml" href="/favicon.svg">
  <style>
    :root { color-scheme: dark; font-family: Inter, ui-sans-serif, system-ui, sans-serif; }
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; color: #f8fafc; background: #09090b; overflow-x: hidden; }
    body::before { content: ""; position: fixed; inset: -30%; z-index: -1; background: radial-gradient(circle at 30% 30%, #6d28d966, transparent 25%), radial-gradient(circle at 70% 65%, #0891b244, transparent 25%); filter: blur(70px); }
    header { padding: 24px clamp(24px, 5vw, 72px); }
    .brand { display: flex; align-items: center; gap: 12px; font-weight: 800; letter-spacing: -.03em; font-size: 20px; }
    .mark { width: 36px; height: 36px; border-radius: 12px; }
    main { display: grid; place-items: center; width: min(760px, calc(100% - 40px)); min-height: calc(100vh - 108px); margin: 0 auto; padding-bottom: 80px; text-align: center; }
    .card { width: 100%; padding: clamp(48px, 9vw, 88px) 24px; border: 1px solid #ffffff20; border-radius: 28px; background: #18181b99; backdrop-filter: blur(24px); box-shadow: 0 30px 80px #0008; }
    .logo { width: min(320px, 80%); height: auto; margin: 0 auto 28px; display: block; }
    h1 { margin: 0; font-size: clamp(40px, 8vw, 74px); letter-spacing: -.06em; line-height: .98; }
    p { max-width: 500px; margin: 22px auto 36px; color: #a1a1aa; font-size: clamp(16px, 2vw, 19px); line-height: 1.6; }
    .login { display: inline-block; padding: 14px 24px; border-radius: 12px; color: #fff; background: linear-gradient(135deg, #7c3aed, #0891b2); box-shadow: 0 16px 40px #7c3aed55; font-weight: 700; text-decoration: none; }
    .login:hover { filter: brightness(1.1); }
    .login:focus-visible { outline: 3px solid #a78bfa; outline-offset: 4px; }
    @media (max-width: 560px) { header { padding: 20px; } main { min-height: calc(100vh - 96px); } }
  </style>
</head>
<body>
  <header><div class="brand"><img class="mark" src="/favicon.svg" alt="" width="36" height="36">Dumpbox</div></header>
  <main>
    <section class="card">
      <img class="logo" src="/assets/logo.svg" alt="Dumpbox">
      <h1>Your files.<br>Your space.</h1>
      <p>Sign in securely to upload files directly to your private folder.</p>
      <a class="login" href="login">Sign in</a>
    </section>
  </main>
</body>
</html>`

const indexHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Dumpbox</title>
  <link rel="icon" type="image/svg+xml" href="/favicon.svg">
  <style>
    :root { color-scheme: dark; font-family: Inter, ui-sans-serif, system-ui, sans-serif; }
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; color: #f8fafc; background: #09090b; overflow-x: hidden; }
    body::before { content: ""; position: fixed; inset: -30%; z-index: -1; background: radial-gradient(circle at 30% 30%, #6d28d966, transparent 25%), radial-gradient(circle at 70% 65%, #0891b244, transparent 25%); filter: blur(70px); }
    header { display: flex; align-items: center; justify-content: space-between; padding: 24px clamp(24px, 5vw, 72px); }
    .brand { display: flex; align-items: center; gap: 12px; font-weight: 800; letter-spacing: -.03em; font-size: 20px; }
    .mark { width: 36px; height: 36px; border-radius: 12px; }
    .user { display: flex; gap: 12px; align-items: center; color: #a1a1aa; font-size: 14px; }
    button { color: inherit; font: inherit; }
    .logout { border: 1px solid #ffffff1a; border-radius: 10px; background: #ffffff0a; padding: 8px 12px; cursor: pointer; }
    main { width: min(760px, calc(100% - 40px)); margin: clamp(40px, 8vh, 96px) auto; text-align: center; }
    h1 { margin: 0; font-size: clamp(40px, 8vw, 74px); letter-spacing: -.06em; line-height: .98; }
    .lead { color: #a1a1aa; font-size: clamp(16px, 2vw, 19px); margin: 22px auto 40px; max-width: 520px; line-height: 1.6; }
    .drop { display: block; position: relative; padding: clamp(44px, 8vw, 76px) 24px; border: 1px solid #ffffff20; border-radius: 28px; background: #18181b99; backdrop-filter: blur(24px); box-shadow: 0 30px 80px #0008; transition: .2s ease; cursor: pointer; }
    .drop::after { content: ""; position: absolute; inset: 8px; pointer-events: none; border: 1px dashed #ffffff24; border-radius: 21px; }
    .drop.dragging { border-color: #a78bfa; transform: scale(1.01); background: #27203acc; }
    .icon { width: 64px; height: 64px; margin: 0 auto 20px; display: grid; place-items: center; border-radius: 20px; background: linear-gradient(135deg, #7c3aed, #0891b2); font-size: 30px; box-shadow: 0 16px 40px #7c3aed55; }
    .drop strong { display: block; font-size: 20px; }
    .drop span { display: block; color: #71717a; margin-top: 8px; }
    input { display: none; }
    .queue { margin-top: 24px; display: grid; gap: 12px; text-align: left; }
    .file { padding: 16px; border: 1px solid #ffffff14; border-radius: 16px; background: #18181bcc; }
    .row { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
    .filename { overflow: hidden; white-space: nowrap; text-overflow: ellipsis; font-weight: 600; }
    .status { flex: 0 0 auto; color: #a1a1aa; font-size: 13px; }
    .bar { height: 5px; margin-top: 12px; overflow: hidden; border-radius: 99px; background: #ffffff10; }
    .fill { width: 0; height: 100%; border-radius: inherit; background: linear-gradient(90deg, #8b5cf6, #06b6d4); transition: width .15s; }
    .done .status { color: #34d399; } .failed .status { color: #fb7185; }
    @media (max-width: 560px) { .user > span { display: none; } header { padding: 20px; } main { margin-top: 44px; } }
  </style>
</head>
<body>
  <header>
    <div class="brand"><img class="mark" src="/favicon.svg" alt="" width="36" height="36">Dumpbox</div>
    <div class="user"><span>{{.Name}}</span><form action="logout" method="post"><button class="logout">Sign out</button></form></div>
  </header>
  <main>
    <h1>Drop it here.</h1>
    <p class="lead">A quiet place for your files. They stream straight to your private folder—no size surprises, no unnecessary copies.</p>
    <label class="drop" id="drop">
      <input type="file" id="picker" multiple>
      <div class="icon">↑</div>
      <strong>Choose files or drag them here</strong>
      <span>Uploads begin automatically</span>
    </label>
    <section class="queue" id="queue" aria-live="polite"></section>
  </main>
  <script>
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
  </script>
</body>
</html>`

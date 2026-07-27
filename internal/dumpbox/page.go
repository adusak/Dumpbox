package dumpbox

var Version = "development"

const landingHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Dumpbox</title>
  <link rel="icon" type="image/svg+xml" href="/favicon.svg">
  <link rel="stylesheet" href="/assets/app.css">
</head>
<body class="landing">
  <header><div class="brand"><img class="mark" src="/favicon.svg" alt="" width="36" height="36">Dumpbox <span class="version">{{.Version}}</span></div></header>
  <main>
    <section class="card">
      <img class="logo" src="/assets/logo.svg" alt="Dumpbox">
      <h1>Deliver files.<br>Securely.</h1>
      <p>Sign in to send files directly to this server.</p>
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
  <link rel="stylesheet" href="/assets/app.css">
  <script src="/assets/app.js" defer></script>
</head>
<body class="application">
  <header>
    <div class="brand"><img class="mark" src="/favicon.svg" alt="" width="36" height="36">Dumpbox <span class="version">{{.Version}}</span></div>
    <div class="user"><span>{{.Name}}</span><form action="logout" method="post"><button class="logout">Sign out</button></form></div>
  </header>
  <main>
    <h1>Drop it here.</h1>
    <p class="lead">Send files directly to this server. Uploads are authenticated and stream straight to their destination.</p>
    <label class="drop" id="drop">
      <input type="file" id="picker" multiple>
      <div class="icon">↑</div>
      <strong>Choose files or drag them here</strong>
      <span>Uploads begin automatically</span>
    </label>
    <section class="queue" id="queue" aria-live="polite"></section>
  </main>
</body>
</html>`

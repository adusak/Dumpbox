package dumpbox

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
  <link rel="stylesheet" href="/assets/app.css">
  <script src="/assets/app.js" defer></script>
</head>
<body class="application">
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
</body>
</html>`

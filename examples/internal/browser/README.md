# Browser example

This directory contains an example use of gRPC-Gateway with web browsers.
The following commands run the integration tests in headless Chrome.

```shell-session
$ npm install -g gulp-cli
$ npm install
$ gulp
```

Puppeteer downloads the Chrome binary used by the headless test task.

## Other examples

### Very simple example

Run

```shell-session
$ gulp bower
$ gulp backends
```

then, open `index.html`.

### Integration test with your browser

Run

```shell-session
$ gulp serve
```

then, open `http://localhost:8000` with your browser.

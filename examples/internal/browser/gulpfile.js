"use strict";

const gulp = require('gulp');

const path = require('path');

const bower = require('gulp-bower');
const exit = require('gulp-exit');
const shell = require('gulp-shell');
const jasmineBrowser = require('gulp-jasmine-browser');
const webpack = require('webpack-stream');
const child = require('child_process');

gulp.task('bower', function () {
  return bower();
});

gulp.task('server', shell.task([
  'go build -o bin/example-server github.com/grpc-ecosystem/grpc-gateway/v2/examples/internal/cmd/example-grpc-server',
]));

gulp.task('gateway', shell.task([
  'go build -o bin/example-gw github.com/grpc-ecosystem/grpc-gateway/v2/examples/internal/cmd/example-gateway-server',
]));

gulp.task('serve-server', gulp.series('server', function startServer(done) {
  const server = child.spawn('bin/example-server', [], { stdio: 'inherit' });
  process.on('exit', function () {
    server.kill();
  });
  done();
}));

gulp.task('serve-gateway', gulp.series('gateway', 'serve-server', function startGateway(done) {
  const gateway = child.spawn('bin/example-gw', [
    '--openapi_dir', path.join(__dirname, "../proto/examplepb"),
  ], { stdio: 'inherit' });
  process.on('exit', function () {
    gateway.kill();
  });
  done();
}));

gulp.task('backends', gulp.series('serve-gateway'));

const specFiles = ['*.spec.js'];
gulp.task('test', gulp.series('backends', function runTests() {
  return gulp.src(specFiles)
    .pipe(webpack({ output: { filename: 'spec.js' } }))
    .pipe(jasmineBrowser.specRunner({
      console: true,
      sourceMappedStacktrace: true,
    }))
    .pipe(jasmineBrowser.headless({
      driver: 'chrome',
      findOpenPort: true,
      catch: true,
      throwFailures: true,
    }))
    .pipe(exit());
}));

gulp.task('serve', gulp.series('backends', function serveBrowser() {
  const JasminePlugin = require('gulp-jasmine-browser/webpack/jasmine-plugin');
  const plugin = new JasminePlugin();

  return gulp.src(specFiles)
    .pipe(webpack({
      output: { filename: 'spec.js' },
      watch: true,
      plugins: [plugin],
    }))
    .pipe(jasmineBrowser.specRunner({
      sourceMappedStacktrace: true,
    }))
    .pipe(jasmineBrowser.server({
      port: 8000,
      whenReady: plugin.whenReady,
    }));
}));

gulp.task('default', gulp.series('test'));

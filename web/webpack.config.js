const { readFileSync } = require('node:fs')
const { join } = require('node:path')

;('use strict')

// The dashboard is compiled into the server binary, so the output goes
// straight into the directory that internal/frontend embeds. There is no
// separate deploy step and nothing is served from a CDN: a self-hosted mail
// server should not depend on anybody else's infrastructure to render its own
// interface.

const path = require('path')
const HtmlWebpackPlugin = require('html-webpack-plugin')
const MiniCssExtractPlugin = require('mini-css-extract-plugin')
const CopyWebpackPlugin = require('copy-webpack-plugin')

const production = process.env.NODE_ENV === 'production'
const output = path.resolve(__dirname, '../internal/frontend/static')

// developmentBackend reads listen.http out of dev/teanode.yaml, so changing
// the port there does not silently leave the dev server proxying to the old
// one — which is a confusing failure, because the dashboard loads fine and
// only the API is missing.
function developmentBackend() {
  if (process.env.TEANODE_DEV_BACKEND) {
    return process.env.TEANODE_DEV_BACKEND
  }

  const fallback = 'http://127.0.0.1:10081'
  try {
    const configuration = readFileSync(join(__dirname, '..', 'dev', 'teanode.yaml'), 'utf8')
    // The http address inside the listen block, for example "  http: :8833".
    const listen = configuration.match(/^listen:\n(?:[ \t]+.*\n)*/m)
    const address = listen && listen[0].match(/^[ \t]+http:[ \t]*"?([^"\n]*)"?$/m)
    if (!address || !address[1].trim()) {
      return fallback
    }
    const [host, port] = address[1].trim().split(':')
    return `http://${host || '127.0.0.1'}:${port}`
  } catch {
    // No dev configuration yet; `make dev-backend` writes one.
    return fallback
  }
}

module.exports = {
  mode: production ? 'production' : 'development',
  devtool: production ? false : 'inline-source-map',
  entry: {
    teanode: './src/index.tsx',
    // What a page the agent makes may link: the helper and the look, at
    // fixed unhashed names, since the page is written by the model from
    // those addresses. The chart library is copied below.
    artifact: ['./src/artifact/artifact.js', './src/artifact/artifact.css'],
  },
  module: {
    rules: [
      { test: /\.tsx?$/, use: 'ts-loader', exclude: /node_modules/ },
      { test: /\.css$/, use: [MiniCssExtractPlugin.loader, 'css-loader'] },
      { test: /\.(png|svg|ico)$/, type: 'asset/resource' },
    ],
  },
  resolve: { extensions: ['.tsx', '.ts', '.js'] },
  output: {
    path: output,
    publicPath: '/',
    // Hashed names, because the server caches them for a year: a file whose
    // name says what is in it can be. The exception is the artifact helper,
    // which a page written by the model links by a fixed address.
    filename: (pathData) =>
      pathData.chunk.name === 'artifact' ? 'assets/artifact.js' : production ? '[name].[contenthash].js' : '[name].js',
    // A chunk fetched on demand: a page somebody has not opened, a catalog in
    // a language they do not read. The hash sits right before the extension
    // because that is where the server looks for it (internal/frontend).
    chunkFilename: production ? '[name].[contenthash].js' : '[name].js',
    clean: true,
  },
  optimization: {
    // Three reasons the dashboard is not one file. Libraries change on their
    // own schedule, not this server's, so they are cached across releases
    // rather than downloaded again with every one of them. A page nobody has
    // opened -- the domain editors, the server's own settings -- is fetched
    // when it is opened. And a catalog is one language, not three.
    //
    // The artifact helper stays whole: it is one small file linked by name
    // from a page this server did not write, and it cannot ask for a second.
    runtimeChunk: production ? 'single' : false,
    splitChunks: {
      chunks: (chunk) => chunk.name !== 'artifact',
      cacheGroups: {
        vendor: {
          test: /[\\/]node_modules[\\/]/,
          name: 'vendor',
          chunks: (chunk) => chunk.name !== 'artifact',
          enforce: true,
        },
      },
    },
  },
  plugins: [
    // excludeChunks rather than chunks: what the dashboard's entry is split
    // into belongs on the page, and naming the entry alone left the runtime
    // and the libraries off it.
    new HtmlWebpackPlugin({
      template: './public/index.html',
      favicon: './public/favicon.ico',
      excludeChunks: ['artifact'],
    }),
    new MiniCssExtractPlugin({
      filename: (pathData) =>
        pathData.chunk.name === 'artifact'
          ? 'assets/artifact.css'
          : production
            ? '[name].[contenthash].css'
            : '[name].css',
      chunkFilename: production ? '[name].[contenthash].css' : '[name].css',
    }),
    // Webpack empties the output directory first, which would delete the
    // committed placeholder that lets go:embed work on a clean checkout.
    new CopyWebpackPlugin({
      patterns: [
        { from: 'public/.gitkeep', to: '.gitkeep', toType: 'file', noErrorOnMissing: true },
        { from: 'node_modules/echarts/dist/echarts.min.js', to: 'assets/echarts.min.js' },
      ],
    }),
  ],
  devServer: {
    host: '127.0.0.1',
    port: 10000,
    // A single page application: every route is served by index.html, and the
    // router works out what to draw. Without this, refreshing on
    // /domains/01K... asks the dev server for a file that does not exist.
    historyApiFallback: true,
    // Proxied to whatever `make dev-backend` is actually listening on, read
    // from the configuration it runs with rather than written here twice.
    // Everything the server answers that is not the dashboard's own files.
    // /media and /.well-known are outside the API prefix on purpose — they
    // are fetched by mail programs and by receiving mail systems, which have
    // no session — and a dev server that proxied only /api showed a broken
    // picture for both.
    proxy: [
      { context: ['/api'], target: developmentBackend() },
      { context: ['/media'], target: developmentBackend() },
      { context: ['/.well-known'], target: developmentBackend() },
    ],
  },
}

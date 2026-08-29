const { readFileSync } = require('node:fs')
const { join } = require('node:path')

'use strict'

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
  entry: './src/index.tsx',
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
    filename: production ? 'teanode.[contenthash].js' : 'teanode.js',
    clean: true,
  },
  plugins: [
    new HtmlWebpackPlugin({ template: './public/index.html', favicon: './public/favicon.ico' }),
    new MiniCssExtractPlugin({ filename: production ? 'teanode.[contenthash].css' : 'teanode.css' }),
    // Webpack empties the output directory first, which would delete the
    // committed placeholder that lets go:embed work on a clean checkout.
    new CopyWebpackPlugin({
      patterns: [{ from: 'public/.gitkeep', to: '.gitkeep', toType: 'file', noErrorOnMissing: true }],
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
    proxy: [{ context: ['/api'], target: developmentBackend() }],
  },
}

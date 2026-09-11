'use strict'

// The browser extension, built the way the dashboard is: its scripts
// bundled, its stylesheet with the dashboard's tokens inlined, the manifest
// and the options page copied beside them. The result in extension/dist is
// what a person loads unpacked; nothing in it is served by the server.

const path = require('path')
const MiniCssExtractPlugin = require('mini-css-extract-plugin')
const CopyWebpackPlugin = require('copy-webpack-plugin')

module.exports = {
  mode: 'production',
  devtool: false,
  context: path.resolve(__dirname, 'extension'),
  entry: {
    background: './src/background.js',
    options: ['./src/options.js', './src/options.css'],
    panel: './src/panel.js',
  },
  module: {
    rules: [{ test: /\.css$/, use: [MiniCssExtractPlugin.loader, 'css-loader'] }],
  },
  output: {
    path: path.resolve(__dirname, 'extension/dist'),
    filename: '[name].js',
    clean: true,
  },
  optimization: {
    // A service worker and an options page each want one plain file.
    splitChunks: false,
    runtimeChunk: false,
  },
  plugins: [
    new MiniCssExtractPlugin({ filename: '[name].css' }),
    new CopyWebpackPlugin({
      patterns: [
        { from: 'manifest.json', to: 'manifest.json' },
        { from: 'src/options.html', to: 'options.html' },
      ],
    }),
  ],
}

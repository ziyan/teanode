'use strict'

// The browser extension, built the way the dashboard is: its scripts
// bundled, its stylesheet with the dashboard's tokens inlined, the manifest
// and the options page copied beside them. The result in extension/dist is
// what a person loads unpacked; nothing in it is served by the server.
//
// The Passwords code is TypeScript and shares the dashboard's own encryption
// (src/passwords, src/pages/passwords/passwordsProtocol.ts), compiled with the
// same ts-loader rule as webpack.config.js; the older scripts stay JavaScript.
// Each page of the extension's own (the button's dropdown, the side panel, the
// passkey question, the clipboard helper) is an entry with its own file, and
// the two content scripts are entries of their own: the manifest names them,
// and nothing is shared between worlds.

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
    sidepanel: './src/sidepanel.js',
    // Run in every page: the isolated world script, and the one that runs in
    // the page's own world to replace the passkey calls.
    content: './src/passwords/content.ts',
    mainWorld: './src/passwords/mainWorld.ts',
    // The extension's own pages for Passwords.
    passwords: ['./src/passwords/passwords.ts', './src/passwords/passwords.css'],
    prompt: ['./src/passwords/prompt.ts', './src/passwords/passwords.css'],
    offscreen: './src/passwords/offscreen.ts',
  },
  module: {
    rules: [
      { test: /\.tsx?$/, use: 'ts-loader', exclude: /node_modules/ },
      { test: /\.css$/, use: [MiniCssExtractPlugin.loader, 'css-loader'] },
    ],
  },
  resolve: { extensions: ['.tsx', '.ts', '.js'] },
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
        { from: 'src/passwords/passwords.html', to: 'passwords.html' },
        { from: 'src/sidepanel.html', to: 'sidepanel.html' },
        { from: 'src/passwords/prompt.html', to: 'prompt.html' },
        { from: 'src/passwords/offscreen.html', to: 'offscreen.html' },
        { from: 'icons', to: 'icons' },
      ],
    }),
  ],
}

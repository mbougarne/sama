const path = require('node:path');
const HtmlWebpackPlugin = require('html-webpack-plugin');
const MiniCssExtractPlugin = require('mini-css-extract-plugin');

module.exports = (_env, argv) => ({
  mode: argv.mode || 'development',
  entry: './src/index.tsx',
  output: {
    path: path.resolve(__dirname, 'dist'),
    filename: '[name].[contenthash].js',
    publicPath: '/',
    clean: true,
  },
  resolve: { extensions: ['.tsx', '.ts', '.js'] },
  module: {
    rules: [
      { test: /\.tsx?$/, use: 'ts-loader', exclude: /node_modules/ },
      { test: /\.css$/, use: [MiniCssExtractPlugin.loader, 'css-loader'] },
    ],
  },
  plugins: [
    new HtmlWebpackPlugin({ template: './public/index.html' }),
    new MiniCssExtractPlugin({ filename: '[name].[contenthash].css' }),
  ],
  devtool: argv.mode === 'production' ? false : 'source-map',
  optimization: { splitChunks: { chunks: 'all' }, runtimeChunk: 'single' },
  devServer: {
    host: '127.0.0.1',
    port: 3000,
    historyApiFallback: true,
    proxy: [{ context: ['/api', '/health'], target: 'http://127.0.0.1:8080' }],
  },
});

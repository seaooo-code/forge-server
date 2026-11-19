#!/usr/bin/env node
// 依赖产物下载脚本，供 gitlab_ci 构建的 CI job 使用。Forge 通过 pipeline 变量 FORGE_DEPS 传入需要的 Common 依赖构建，
// 这个脚本把它们下载到 .forge/deps/common/{runtimeVersion}/{platform}/...，并写出 index.json，目录结构与 docker 构建的依赖注入一致。
//
// 用法：node forge-fetch-deps.js [输出目录，默认 .forge/deps/common]
// 环境变量：
//   FORGE_DEPS            Forge 传入的 JSON；不存在时什么都不做（不是 Feature 模块，或没有依赖）
//   FORGE_DEPS_MAX_BYTES  依赖总大小上限，默认 209715200（200 MiB）
//
// 只依赖 Node 内置模块，兼容 Node 14 及以上。
'use strict';

const crypto = require('crypto');
const fs = require('fs');
const http = require('http');
const https = require('https');
const path = require('path');

const PLATFORMS = ['android', 'ios', 'harmonyos'];
const CONCURRENCY = 8;
const ATTEMPTS = 3;
const REQUEST_TIMEOUT_MS = 60 * 1000;

function log(message) {
  console.log(`[forge-deps] ${message}`);
}

function fail(message) {
  console.error(`[forge-deps] 错误：${message}`);
  process.exit(1);
}

function get(url) {
  return new Promise((resolve, reject) => {
    const client = url.startsWith('https:') ? https : http;
    const request = client.get(url, { timeout: REQUEST_TIMEOUT_MS }, (response) => {
      if (response.statusCode !== 200) {
        response.resume();
        reject(new Error(`GET ${url} 返回 ${response.statusCode}`));
        return;
      }
      const chunks = [];
      response.on('data', (chunk) => chunks.push(chunk));
      response.on('end', () => resolve(Buffer.concat(chunks)));
      response.on('error', reject);
    });
    request.on('timeout', () => request.destroy(new Error(`GET ${url} 超时`)));
    request.on('error', reject);
  });
}

async function getWithRetry(url) {
  let lastError;
  for (let attempt = 1; attempt <= ATTEMPTS; attempt++) {
    try {
      return await get(url);
    } catch (error) {
      lastError = error;
      if (attempt < ATTEMPTS) {
        await new Promise((resolve) => setTimeout(resolve, attempt * 500));
      }
    }
  }
  throw lastError;
}

// 清单里的路径来自远端，必须是相对路径且不能越界。
function safeRelativePath(value) {
  const cleaned = path.posix.normalize(String(value || '').trim());
  if (!cleaned || cleaned === '.' || cleaned.startsWith('/') || cleaned.startsWith('..') || cleaned.includes('\\')) {
    return null;
  }
  return cleaned;
}

async function runPool(tasks, concurrency) {
  let next = 0;
  const workers = Array.from({ length: Math.min(concurrency, tasks.length) }, async () => {
    while (next < tasks.length) {
      const task = tasks[next++];
      await task();
    }
  });
  await Promise.all(workers);
}

async function main() {
  const raw = process.env.FORGE_DEPS;
  if (!raw) {
    log('没有 FORGE_DEPS，跳过依赖下载');
    return;
  }
  let deps;
  try {
    deps = JSON.parse(raw);
  } catch (error) {
    fail(`FORGE_DEPS 不是合法的 JSON：${error.message}`);
  }

  const outputDir = path.resolve(process.argv[2] || path.join('.forge', 'deps', 'common'));
  const maxBytes = Number(process.env.FORGE_DEPS_MAX_BYTES || 200 * 1024 * 1024);
  const items = Array.isArray(deps.items) ? deps.items : [];
  fs.mkdirSync(outputDir, { recursive: true });

  const index = { moduleId: deps.moduleId, environment: deps.environment, items: [] };
  if (items.length === 0) {
    log(`Common 模块 ${deps.moduleId} 在环境 ${deps.environment} 下没有生效中的默认路由，只写入空的 index.json`);
  }

  const downloads = [];
  let totalBytes = 0;
  for (const item of items) {
    const runtimeVersion = String(item.runtimeVersion || '');
    if (!/^[A-Za-z0-9._-]+$/.test(runtimeVersion) || runtimeVersion === '.' || runtimeVersion === '..' || runtimeVersion === 'index.json') {
      fail(`runtimeVersion ${JSON.stringify(runtimeVersion)} 不能作为目录名`);
    }
    const manifest = JSON.parse((await getWithRetry(item.manifestUrl)).toString('utf8'));
    const platforms = new Set();
    let fileCount = 0;
    for (const file of manifest.files || []) {
      const relative = safeRelativePath(file.path);
      if (!relative) {
        fail(`依赖构建 #${item.buildNumber} 的清单里有非法路径 ${JSON.stringify(file.path)}`);
      }
      const platform = relative.split('/')[0];
      // 只注入平台目录下的文件，forge.json 这类根目录文件不注入。
      if (!relative.includes('/') || !PLATFORMS.includes(platform) || !(item.platforms || []).includes(platform)) {
        continue;
      }
      if (!file.sha256 || !(file.sizeBytes >= 0) || !file.objectKey) {
        fail(`依赖构建 #${item.buildNumber} 的清单文件 ${relative} 缺少 sha256、大小或 objectKey`);
      }
      totalBytes += file.sizeBytes;
      if (totalBytes > maxBytes) {
        fail(`依赖产物总大小超过上限 ${maxBytes} 字节（FORGE_DEPS_MAX_BYTES）`);
      }
      platforms.add(platform);
      fileCount++;
      const target = path.join(outputDir, runtimeVersion, ...relative.split('/'));
      const url = `${String(item.baseUrl).replace(/\/+$/, '')}/${String(file.objectKey).replace(/^\/+/, '')}`;
      downloads.push(async () => {
        const body = await getWithRetry(url);
        if (body.length !== file.sizeBytes) {
          throw new Error(`${relative} 大小不一致：清单 ${file.sizeBytes}，实际 ${body.length}`);
        }
        const digest = crypto.createHash('sha256').update(body).digest('hex');
        if (digest !== file.sha256) {
          throw new Error(`${relative} 的 sha256 不一致`);
        }
        fs.mkdirSync(path.dirname(target), { recursive: true });
        fs.writeFileSync(target, body, { flag: 'wx' });
      });
    }
    index.items.push({
      runtimeVersion,
      buildRecordId: item.buildRecordId,
      buildNumber: item.buildNumber,
      commitSha: item.commitSha,
      platforms: Array.from(platforms).sort(),
    });
    log(`依赖：runtimeVersion ${runtimeVersion}，构建 #${item.buildNumber}，平台 ${Array.from(platforms).sort().join(',')}，${fileCount} 个文件`);
  }

  try {
    await runPool(downloads, CONCURRENCY);
  } catch (error) {
    fail(`下载依赖产物失败：${error.message}`);
  }
  fs.writeFileSync(path.join(outputDir, 'index.json'), JSON.stringify(index, null, 2) + '\n');
  log(`完成：${downloads.length} 个文件，${totalBytes} 字节，输出目录 ${outputDir}`);
}

main().catch((error) => fail(error.stack || error.message));

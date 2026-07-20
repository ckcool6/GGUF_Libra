import { marked } from 'marked';
import twemoji from 'twemoji';
import hljs from 'highlight.js';
import katex from 'katex';
import 'katex/dist/katex.min.css';
import 'highlight.js/styles/atom-one-dark.min.css';

// ==================================== init & extensions load =============================
// 自定义 marked 扩展，用来精确拦截 $$ 和 $
const latexExtension = {
    name: 'inlineLatex',
    level: 'inline',
    start(src) { return src.indexOf('$'); },
    tokenizer(src) {
        // 1. 优先匹配块级公式 $$...$$
        const blockMatch = /^\$\$\s*([\s\S]*?)\s*\$\$/.exec(src);
        if (blockMatch) {
            return {
                type: 'inlineLatex',
                raw: blockMatch[0],
                text: blockMatch[1],
                displayMode: true
            };
        }
        // 2. 匹配行内公式 $...$
        const inlineMatch = /^\$([^\$\n]+?)\$/.exec(src);
        if (inlineMatch) {
            return {
                type: 'inlineLatex',
                raw: inlineMatch[0],
                text: inlineMatch[1],
                displayMode: false
            };
        }
    },
    renderer(token) {
        try {
            return katex.renderToString(token.text, {
                displayMode: token.displayMode,
                throwOnError: false
            });
        } catch (err) {
            return token.raw;
        }
    }
};

marked.use({ extensions: [latexExtension] });

marked.setOptions({
    highlight: (code, lang) => {
        if (lang && hljs.getLanguage(lang)) return hljs.highlight(code, { language: lang }).value;
        return hljs.highlightAuto(code).value;
    },
    breaks: true,
    gfm: true
});

window.onload = () => {
    const savedTheme = localStorage.getItem('theme');

    if (savedTheme === 'dark' || (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)) {
        document.documentElement.classList.add('dark-mode-active', 'dark');
        updateModeIcon(true);
    } else {
        document.documentElement.classList.remove('dark-mode-active', 'dark');
        updateModeIcon(false);
    }

    loadHistory();

    input.addEventListener('keydown', e => {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            if (sendBtn.classList.contains('is-loading')) {
                if (chatAbortController) chatAbortController.abort();
                return;
            }
            send();
        }
    });
    checkLlamaConnection();
    setInterval(checkLlamaConnection, 10000);
};

async function loadHistory() {
    try {
        const res = await fetch('/api/history');
        const data = await res.json();
        if (data && data.length > 0) {
            chatBox.innerHTML = '';

            data.forEach(m => {
                const isUser = m.role === 'user';
                const content = isUser ? m.content.replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/\n/g, "<br>") : marked.parse(m.content);
                // 修复：去掉用户气泡上的 markdown-body 类名
                const html = `
            <div class="flex ${isUser ? 'justify-end' : 'justify-start'} mb-4">
                <div class="${isUser ? 'user-bubble' : 'ai-bubble markdown-body'} p-4 rounded-2xl max-w-[90%] shadow-sm">
                    ${content}
                </div>
            </div>`;
                chatBox.insertAdjacentHTML('beforeend', html);
            });

            chatBox.querySelectorAll('.ai-bubble pre code').forEach(el => hljs.highlightElement(el));
            twemoji.parse(chatBox, { folder: 'svg', ext: '.svg' });
            chatBox.scrollTop = chatBox.scrollHeight;
        }
    } catch (e) {
        console.error("加载历史记录失败:", e);
    }
}

// ==================================== send messages ==============================================
let chatAbortController = null;

const chatBox = document.getElementById('chat-box');
const input = document.getElementById('user-input');
const loading = document.getElementById('ai-loading-template');
const sendBtn = document.getElementById('send-btn');

// 自适应输入框高度逻辑
if (input) {
    input.addEventListener('input', () => {
        input.style.height = 'auto';
        input.style.height = input.scrollHeight + 'px';
    });
}


async function send() {
    if (sendBtn.classList.contains('is-loading')) {
        if (chatAbortController) chatAbortController.abort();
        return;
    }

    const text = input.value.trim();
    if (!text) return;

    // 改变按钮状态与清空输入
    sendBtn.classList.add('is-loading');
    sendBtn.innerHTML = `<svg class="w-5 h-5 animate-pulse" fill="currentColor" viewBox="0 0 24 24"><path fill-rule="evenodd" d="M4.5 7.5a3 3 0 013-3h9a3 3 0 013 3v9a3 3 0 01-3 3h-9a3 3 0 01-3-3v-9z" clip-rule="evenodd" /></svg>`;
    input.value = '';
    input.style.height = 'auto';

    // 渲染用户消息与加载动画
    const safeUserText = text.replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/\n/g, "<br>");
    chatBox.insertAdjacentHTML('beforeend', `<div class="flex justify-end mb-4"><div class="user-bubble p-4 rounded-2xl max-w-[85%] shadow-sm">${safeUserText}</div></div>`);
    chatBox.appendChild(loading);
    loading.classList.remove('hidden');
    chatBox.scrollTo({ top: chatBox.scrollHeight, behavior: 'smooth' });

    chatAbortController = new AbortController();

    try {
        // 发起请求
        const response = await fetch('/api/chat', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            signal: chatAbortController.signal,
            body: JSON.stringify({
                message: text,
                custom_url: localStorage.getItem('custom_api_url') || '',
                custom_key: localStorage.getItem('custom_api_key') || ''
            })
        });

        // 校验状态
        if (!response.ok) {
            const errorMsg = await extractErrorMessage(response);
            throw new Error(errorMsg);
        }

        // 消费流数据
        await handleStreamResponse(response);

    } catch (err) {
        if (err.name === 'AbortError') {
            console.log("用户中止了 AI 的回复");
        } else {
            loading.classList.add('hidden');
            const errorHtml = `
                <div class="flex justify-start mb-4">
                    <div class="p-4 rounded-2xl bg-red-50 text-red-600 border border-red-200 text-sm shadow-sm max-w-[90%]">
                        <div class="font-bold mb-1">⚠️ 遇到系统错误</div>
                        <div>${err.message || "连接错误，请检查后端。"}</div>
                    </div>
                </div>`;
            chatBox.insertAdjacentHTML('beforeend', errorHtml);
            chatBox.scrollTop = chatBox.scrollHeight;
            checkLlamaConnection();
        }
    } finally {
        loading.classList.add('hidden');
        sendBtn.classList.remove('is-loading');
        sendBtn.innerHTML = '发送';
        chatAbortController = null;
    }
}

// 核心处理器对象：查表法替代多重 if
let isRenderPending = false;

const streamChunkHandlers = {
    model: (json, ctx) => {
        ctx.modelName = json.model.split('/').pop().split('\\').pop();
    },
    usage: (json, ctx) => {
        if (json.usage && json.usage.completion_tokens) {
            ctx.tokenCount = json.usage.completion_tokens;
            ctx.hasOfficialUsage = true;
        }
    },
    choices: (json, ctx) => {
        const content = json.choices?.[0]?.delta?.content || "";
        if (!content) return;

        if (!ctx.startTime) ctx.startTime = Date.now();
        if (!ctx.hasOfficialUsage) ctx.tokenCount++;

        ctx.fullText += content;

        // 首次收到消息：立即创建气泡框架
        if (ctx.isFirstChunk) {
            loading.classList.add('hidden');
            ctx.currentBubbleId = 'ai-' + Date.now();
            const html = `
                <div class="flex justify-start mb-4">
                    <div class="flex flex-col max-w-[90%]">
                        <div id="${ctx.currentBubbleId}" class="ai-bubble p-4 rounded-2xl shadow-sm markdown-body">
                            ${marked.parse(ctx.fullText)}
                        </div>
                        <div id="meta-${ctx.currentBubbleId}" class="flex items-center gap-3 px-2 mt-1.5 text-xs text-gray-400 dark:text-gray-400 font-mono opacity-80">
                            <span class="bg-gray-100 dark:bg-white/5 px-1.5 py-0.5 rounded text-[11px]">${ctx.modelName}</span>
                            <span id="speed-${ctx.currentBubbleId}">⏱️ 正在计算...</span>
                        </div>
                    </div>
                </div>`;
            chatBox.insertAdjacentHTML('beforeend', html);
            ctx.aiBubbleDiv = document.getElementById(ctx.currentBubbleId);
            ctx.isFirstChunk = false;
        }

        // 流传输过程中的高频渲染：使用 rAF 节流，避免阻塞主线程
        if (!isRenderPending) {
            isRenderPending = true;
            requestAnimationFrame(() => {
                if (ctx.aiBubbleDiv) {
                    ctx.aiBubbleDiv.innerHTML = marked.parse(ctx.fullText);
                    // 传输中只做必要的代码高亮
                    ctx.aiBubbleDiv.querySelectorAll('pre code').forEach(el => hljs.highlightElement(el));
                    updateStreamingSpeed(ctx.currentBubbleId, ctx.startTime, ctx.tokenCount);
                    chatBox.scrollTop = chatBox.scrollHeight;
                }
                isRenderPending = false;
            });
        }
    }
};

// 辅助函数：更新打字机速度与耗时提示
function updateStreamingSpeed(currentBubbleId, startTime, tokenCount) {
    if (!startTime) return;
    const elapsed = (Date.now() - startTime) / 1000;
    if (elapsed > 0) {
        const speed = (tokenCount / elapsed).toFixed(1);
        const speedSpan = document.getElementById(`speed-${currentBubbleId}`);
        if (speedSpan) {
            speedSpan.innerText = ` ${elapsed.toFixed(1)}s / ${speed} t/s`;
        }
    }
}

// 辅助函数：流完全结束后的最终全量高亮与统计
function finalizeAiBubble(ctx) {
    if (!ctx.aiBubbleDiv) return;

    // 一次性执行高亮和表情包解析
    ctx.aiBubbleDiv.querySelectorAll('pre code').forEach(el => hljs.highlightElement(el));
    twemoji.parse(ctx.aiBubbleDiv, { folder: 'svg', ext: '.svg' });

    // 展示最终精确的 tokens 统计
    if (ctx.startTime && ctx.currentBubbleId) {
        const elapsed = (Date.now() - ctx.startTime) / 1000;
        const speed = (ctx.tokenCount / (elapsed || 1)).toFixed(1);
        const speedSpan = document.getElementById(`speed-${ctx.currentBubbleId}`);
        if (speedSpan) {
            speedSpan.innerHTML = ` takes ${elapsed.toFixed(1)}s  (total ${ctx.tokenCount} tokens / speed ${speed} t/s)`;
        }
    }
}

// 主流式响应处理函数
async function handleStreamResponse(response) {
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";

    // 状态上下文对象：传递给每个处理器，用于共享流状态
    const ctx = {
        fullText: "",
        isFirstChunk: true,
        aiBubbleDiv: null,
        tokenCount: 0,
        startTime: null,
        modelName: "GGUF Model",
        currentBubbleId: "",
        hasOfficialUsage: false
    };

    try {
        while (true) {
            const { done, value } = await reader.read();
            if (done) break;

            buffer += decoder.decode(value, { stream: true });
            const lines = buffer.split('\n');
            buffer = lines.pop();

            for (const line of lines) {
                const trimmed = line.trim();
                if (!trimmed.startsWith('data: ') || trimmed === 'data: [DONE]') continue;

                const json = JSON.parse(trimmed.substring(6));

                // 核心分发逻辑：遍历对象中的 key，存在即处理
                for (const key in streamChunkHandlers) {
                    if (json[key] !== undefined) {
                        streamChunkHandlers[key](json, ctx);
                    }
                }
            }
        }

        // 流结束后的收尾工作
        finalizeAiBubble(ctx);
        chatBox.scrollTop = chatBox.scrollHeight;

    } catch (streamError) {
        console.error("流式读取过程中发生错误:", streamError);
        throw streamError;
    }
}

async function extractErrorMessage(response) {
    let errorText = `请求失败，状态码：${response.status}`;
    try {
        const errJson = await response.json();
        if (errJson.error) return `${errorText} (${errJson.error})`;
        if (errJson.message) return `${errorText} (${errJson.message})`;
    } catch (_) {
        try {
            const text = await response.text();
            if (text) return `${errorText} - ${text}`;
        } catch (_) { }
    }
    return errorText;
}

document.getElementById('send-btn').addEventListener('click', send);

// ============================= buttons ============================================
function downloadChat() {
    const messages = chatBox.querySelectorAll('.user-bubble, .ai-bubble');
    let content = "--- 聊天记录 ---\n\n";
    messages.forEach(el => {
        const role = el.classList.contains('user-bubble') ? "【用户】" : "【AI】";
        content += `${role}\n${el.innerText.trim()}\n\n`;
    });
    const blob = new Blob([content], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `chat-${Date.now()}.txt`;
    a.click();
}

async function newChat() {
    if (confirm("清空所有对话？")) {
        try {
            await fetch('/api/new-chat');
            chatBox.innerHTML = `
                <div class="flex justify-start mb-4">
                    <div class="ai-bubble p-4 rounded-2xl shadow-sm max-w-[90%] markdown-body">
                        你好!
                    </div>
                </div>`;
        } catch (e) {
            console.error("清空对话失败:", e);
        }
    }
}

function toggleDarkMode() {
    document.documentElement.classList.toggle('dark-mode-active');
    const isDark = document.documentElement.classList.toggle('dark');
    localStorage.setItem('theme', isDark ? 'dark' : 'light');
    updateModeIcon(isDark);
}

function updateModeIcon(isDark) {
    document.getElementById('mode-icon').innerHTML = isDark
        ? `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364-6.364l-.707.707M6.343 17.657l-.707.707m12.728 0l-.707-.707M6.343 6.343l-.707-.707M12 8a4 4 0 100 8 4 4 0 000-8z"></path>`
        : `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z"></path>`;
}

document.getElementById('download-btn').addEventListener('click', downloadChat);
document.getElementById('theme-btn').addEventListener('click', toggleDarkMode);
document.getElementById('new-chat-btn').addEventListener('click', newChat);

// ========================= settings page =========================================
const modal = document.getElementById('settings-modal');
const customUrlInput = document.getElementById('custom-url');
const customKeyInput = document.getElementById('custom-key');

document.getElementById('settings-btn').addEventListener('click', () => {
    customUrlInput.value = localStorage.getItem('custom_api_url') || '';
    customKeyInput.value = localStorage.getItem('custom_api_key') || '';
    modal.classList.remove('hidden');
});

document.getElementById('close-settings').addEventListener('click', () => {
    modal.classList.add('hidden');
});

modal.addEventListener('click', (e) => {
    if (e.target === modal) modal.classList.add('hidden');
});

document.getElementById('save-settings').addEventListener('click', () => {
    localStorage.setItem('custom_api_url', customUrlInput.value.trim());
    localStorage.setItem('custom_api_key', customKeyInput.value.trim());
    modal.classList.add('hidden');
});

// ================================= indictor light ==========================================
const statusDot = document.getElementById('status-dot');

// 检测 llama.cpp 连接状态的函数
async function checkLlamaConnection() {
    // 优先获取用户自定义的 API 地址，如果没有则使用你项目默认的后端地址
    const customUrl = localStorage.getItem('custom_api_url') || '';

    let healthUrl = '/health'; // 默认同域路由

    if (customUrl) {
        try {
            // 如果填了自定义的完整的地址，比如 http://127.0.0.1:8080/v1/chat/completions
            // 我们需要把尾部的路径换成 /health
            const urlObj = new URL(customUrl);
            healthUrl = `${urlObj.protocol}//${urlObj.host}/health`;
        } catch (e) {
            console.error("解析自定义 URL 失败:", e);
        }
    }

    try {
        // 设置 3 秒超时，防止接口卡死导致状态一直不更新
        const controller = new AbortController();
        const timeoutId = setTimeout(() => controller.abort(), 3000);

        const res = await fetch(healthUrl, {
            method: 'GET',
            signal: controller.signal
        });

        clearTimeout(timeoutId);

        // llama.cpp 的 /health 接口正常情况下会返回 {"status": "ok"}
        if (res.ok) {
            const data = await res.json();
            if (data.status === 'ok') {
                // 连接成功：变绿，并移除动画
                statusDot.className = "w-2.5 h-2.5 rounded-full bg-emerald-500 transition-colors duration-300 shadow-[0_0_8px_rgba(16,185,129,0.5)]";
                statusDot.title = "已成功连接到 llama.cpp";
                return;
            }
        }
        throw new Error("服务状态异常");

    } catch (err) {
        // 连接失败：变红，并加上闪烁动画提示用户注意
        statusDot.className = "w-2.5 h-2.5 rounded-full bg-rose-500 transition-colors duration-300 shadow-[0_0_8px_rgba(244,63,94,0.5)] animate-pulse";
        statusDot.title = "无法连接到 llama.cpp，请检查后端服务是否启动";
    }
}
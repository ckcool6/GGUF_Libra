import DOMPurify from 'dompurify';
import { marked } from 'marked';
import twemoji from 'twemoji';
import hljs from 'highlight.js';
import katex from 'katex';
import 'katex/dist/katex.min.css';
import 'highlight.js/styles/atom-one-dark.min.css';

// ==================================== 工具函数 =============================================
// 用户纯文本转义与换行处理
function formatUserText(str) {
    // 先做净化，再把换行替换为 <br>
    const cleanStr = DOMPurify.sanitize(str);
    return cleanStr.replace(/\n/g, "<br>");
}

// AI Markdown 渲染与 XSS 边界防护
function safeMarkdownParse(content) {
    const rawHtml = marked.parse(content);
    return DOMPurify.sanitize(rawHtml, {
        ADD_TAGS: ['use', 'path', 'svg'], // 容许 KaTeX / Math 相关的 SVG 标签
        ADD_ATTR: ['target', 'allow']
    });
}

// 智能滚动：只有当用户处于底部附近时才自动滚动
function scrollToBottomIfNear() {
    const isAtBottom = chatBox.scrollHeight - chatBox.scrollTop - chatBox.clientHeight < 120;
    if (isAtBottom) {
        chatBox.scrollTop = chatBox.scrollHeight;
    }
}

// 初始化提示词下拉框
async function initPromptSelect() {
    const select = document.getElementById('prompt-select');
    if (!select) return;

    try {
        const res = await fetch('/api/prompts');
        if (!res.ok) throw new Error(`HTTP 状态异常: ${res.status}`);

        const data = await res.json();

        select.innerHTML = '';
        if (data.prompts && data.prompts.length > 0) {
            data.prompts.forEach(p => {
                const opt = document.createElement('option');
                opt.value = p.id;
                opt.innerText = p.name;
                if (p.id === data.active) opt.selected = true;
                select.appendChild(opt);
            });
            select.disabled = false; // 正常启用下拉框
        } else {
            showPromptError(select, "无可用提示词");
        }

        // 监听切换事件（使用 addEventListener 确保只绑定一次，或者直接覆盖 onchange）
        select.onchange = async (e) => {
            const newId = e.target.value;

            // 1. 如果当前正在生成回复，强行中断请求
            if (chatAbortController) {
                chatAbortController.abort();
                chatAbortController = null;
            }

            // 2. 还原按钮与加载状态
            loading.classList.add('hidden');
            sendBtn.classList.remove('is-loading');
            sendBtn.innerHTML = '发送';

            try {
                // 3. 通知后端切换提示词
                const switchRes = await fetch('/api/switch-prompt', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ id: newId })
                });

                if (!switchRes.ok) throw new Error("切换提示词失败");

                // 4. 读取本地 custom_url，带上参数去调用清空后端对话历史
                const customUrl = localStorage.getItem('custom_api_url') || '';
                await fetch(`/api/new-chat?custom_url=${encodeURIComponent(customUrl)}`);

                // 5. 重置前端 UI
                chatBox.innerHTML = `
                    <div class="flex justify-start mb-8">
                        <div class="ai-bubble p-4 rounded-2xl max-w-[90%] markdown-body">
                            你好！
                        </div>
                    </div>`;

                // 6. 重新刷新 Context 内存计算
                get_ctx_usage();

            } catch (err) {
                console.error("切换提示词或清空对话失败:", err);
            }
        };

    } catch (e) {
        console.error("加载提示词列表失败:", e);
        showPromptError(select, "未连接服务");
    }
}

// 未连接或无数据时的显示提示词辅助函数
function showPromptError(select, message) {
    select.innerHTML = `<option value="" disabled selected>⚠️ ${message}</option>`;
    select.disabled = true;
}

// ==================================== init & extensions load =============================
// 自定义 marked 扩展，用来精确拦截 $$ 和 $
const latexExtension = {
    name: 'inlineLatex',
    level: 'inline',
    start(src) { return src.indexOf('$'); },
    tokenizer(src) {
        // 优先匹配块级公式 $$...$$
        const blockMatch = /^\$\$\s*([\s\S]*?)\s*\$\$/.exec(src);
        if (blockMatch) {
            return {
                type: 'inlineLatex',
                raw: blockMatch[0],
                text: blockMatch[1],
                displayMode: true
            };
        }
        // 匹配行内公式 $...$
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
    initPromptSelect();
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
                return;
            }
            send();
        }
    });
    checkLlamaConnection();
    get_ctx_usage();

    setInterval(() => {
        checkLlamaConnection();
        get_ctx_usage();
    }, 10000);
};

async function loadHistory() {
    try {
        const res = await fetch('/api/history');
        const data = await res.json();
        if (data && data.length > 0) {
            chatBox.innerHTML = '';

            data.forEach(m => {
                const isUser = m.role === 'user';
                const content = isUser ? formatUserText(m.content) : safeMarkdownParse(m.content);

                if (isUser) {
                    const html = `
                        <div class="flex justify-start mb-8">
                            <div class="user-bubble p-4 rounded-2xl max-w-[90%]">
                                ${content}
                            </div>
                        </div>`;
                    chatBox.insertAdjacentHTML('beforeend', html);
                } else {
                    // 为 AI 消息构造 DOM 结构并插入 Notebook Bar
                    const wrapper = document.createElement('div');
                    wrapper.className = 'flex justify-start mb-8';
                    wrapper.innerHTML = `
                        <div class="flex flex-col max-w-[90%] w-full">
                            <div class="ai-bubble p-4 rounded-2xl markdown-body">
                                ${content}
                            </div>
                        </div>`;

                    const notebookBar = createNotebookBar();
                    wrapper.querySelector('.flex-col').appendChild(notebookBar);
                    chatBox.appendChild(wrapper);
                }
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

if (input) {
    input.addEventListener('input', () => {
        input.style.height = 'auto';
        input.style.height = input.scrollHeight + 'px';
    });
}

async function send() {
    if (sendBtn.classList.contains('is-loading')) {
        if (chatAbortController) {
            chatAbortController.abort();
        }
        return;
    }

    const text = input.value.trim();
    if (!text) return;

    sendBtn.classList.add('is-loading');
    sendBtn.innerHTML = `<svg class="w-5 h-5 animate-pulse" fill="currentColor" viewBox="0 0 24 24"><path fill-rule="evenodd" d="M4.5 7.5a3 3 0 013-3h9a3 3 0 013 3v9a3 3 0 01-3 3h-9a3 3 0 01-3-3v-9z" clip-rule="evenodd" /></svg>`;
    input.value = '';
    input.style.height = 'auto';

    const safeUserText = formatUserText(text);
    chatBox.insertAdjacentHTML('beforeend', `<div class="flex justify-start mb-8"><div class="user-bubble p-4 rounded-2xl max-w-[85%]">${safeUserText}</div></div>`);
    loading.classList.remove('hidden');
    chatBox.scrollTo({ top: chatBox.scrollHeight, behavior: 'smooth' });

    chatAbortController = new AbortController();

    try {
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

        if (!response.ok) {
            const errorMsg = await extractErrorMessage(response);
            throw new Error(errorMsg);
        }

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
                        <div>${formatUserText(err.message || "连接错误，请检查后端。")}</div>
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
        get_ctx_usage();
    }
}

// 核心处理器对象
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

        // 1. 将新收到的字符压入 Buffer 缓冲队列
        ctx.charBuffer.push(...content.split(''));

        // 2. 首次收到消息：创建 AI 气泡 DOM
        if (ctx.isFirstChunk) {
            loading.classList.add('hidden');
            ctx.currentBubbleId = 'ai-' + Date.now();
            const html = `
                        <div class="flex justify-start mb-8">
                            <div class="flex flex-col max-w-[90%]">
                                <div id="${ctx.currentBubbleId}" class="ai-bubble p-4 rounded-2xl markdown-body">
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
    }
};

async function handleStreamResponse(response) {
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";

    const ctx = {
        fullText: "",
        displayedText: "", // 已经渲染出来的字符
        charBuffer: [],    // Buffer 字符队列
        isFirstChunk: true,
        aiBubbleDiv: null,
        tokenCount: 0,
        startTime: null,
        modelName: "GGUF Model",
        currentBubbleId: "",
        hasOfficialUsage: false,
        isRenderPending: false
    };

    // 检查并自动补全 Markdown 中未闭合的代码块 (```)
    function fixUnclosedCodeBlocks(markdownText) {
        // 匹配所有的 ```
        const matches = markdownText.match(/```/g);
        // 如果匹配到了奇数个 ```，说明当前有一个代码块处于开启且未闭合状态
        if (matches && matches.length % 2 !== 0) {
            return markdownText + '\n```'; // 临时补齐结尾的 ```
        }
        return markdownText;
    }

    // 启动 Buffer 平滑渲染定时器（按 16ms 频率消费队列，结合 rAF 渲染）
    const bufferTimer = setInterval(() => {
        if (ctx.charBuffer.length > 0 && ctx.aiBubbleDiv && !ctx.isRenderPending) {
            ctx.isRenderPending = true;

            requestAnimationFrame(() => {
                if (ctx.aiBubbleDiv) {
                    // 动态步长：根据堆积量决定消费速度，积压多就一次多吐几个字，避免延迟过大
                    const step = ctx.charBuffer.length > 30 ? 4 : (ctx.charBuffer.length > 10 ? 2 : 1);
                    const chunk = ctx.charBuffer.splice(0, step).join('');
                    ctx.displayedText += chunk;

                    // 在解析前先补齐可能存在的未闭合代码块，让 marked 能正确识别出 <pre><code>
                    const streamMarkdown = fixUnclosedCodeBlocks(ctx.displayedText);
                    ctx.aiBubbleDiv.innerHTML = safeMarkdownParse(streamMarkdown);

                    // 每一帧解析完成后，立刻为当前气泡内的代码块添加高亮
                    ctx.aiBubbleDiv.querySelectorAll('pre code').forEach(el => hljs.highlightElement(el));

                    updateStreamingSpeed(ctx.currentBubbleId, ctx.startTime, ctx.tokenCount);
                    scrollToBottomIfNear();
                }
                ctx.isRenderPending = false;
            });
        }
    }, 16); //

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

                for (const key in streamChunkHandlers) {
                    if (json[key] !== undefined) {
                        streamChunkHandlers[key](json, ctx);
                    }
                }
            }
        }

        // 等待队列里的剩余字符全部消费完毕
        while (ctx.charBuffer.length > 0) {
            await new Promise(r => setTimeout(r, 16));
        }

    } catch (streamError) {
        console.error("流式读取过程中发生错误:", streamError);
        throw streamError;
    } finally {
        clearInterval(bufferTimer); // 消费完成，销毁定时器
        finalizeAiBubble(ctx);
        scrollToBottomIfNear();
    }
}

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

function finalizeAiBubble(ctx) {
    if (!ctx.aiBubbleDiv) return;

    // 1. 进行最终无死角的安全 Markdown 解析，补齐最后一帧
    ctx.aiBubbleDiv.innerHTML = safeMarkdownParse(ctx.fullText);

    // 2. 补齐代码高亮与表情包解析
    ctx.aiBubbleDiv.querySelectorAll('pre code').forEach(el => hljs.highlightElement(el));
    twemoji.parse(ctx.aiBubbleDiv, { folder: 'svg', ext: '.svg' });

    // 3. 统计展示
    if (ctx.startTime && ctx.currentBubbleId) {
        const elapsed = (Date.now() - ctx.startTime) / 1000;
        const speed = (ctx.tokenCount / (elapsed || 1)).toFixed(1);
        const speedSpan = document.getElementById(`speed-${ctx.currentBubbleId}`);
        if (speedSpan) {
            speedSpan.innerHTML = ` takes ${elapsed.toFixed(1)}s  (total ${ctx.tokenCount} tokens / speed ${speed} t/s)`;
        }
    }

    // 4. 追加 Notebook 悬浮控制条
    const notebookBar = createNotebookBar();
    // 插入到消息容器的最下方
    ctx.aiBubbleDiv.parentElement.appendChild(notebookBar);
}

function createNotebookBar() {
    const notebookBar = document.createElement('div');
    notebookBar.className = 'notebook-bar group relative flex flex-col items-center justify-center my-4 opacity-40 hover:opacity-100 transition-opacity duration-200';

    notebookBar.innerHTML = `
        <div class="w-full relative flex items-center justify-center">
            <!-- 背景横线 -->
            <div class="absolute inset-0 flex items-center">
                <div class="w-full border-t border-gray-200 dark:border-gray-800"></div>
            </div>
            
            <!-- 悬浮按钮组 -->
            <div class="relative flex items-center gap-2 bg-white dark:bg-[#1e1f20] px-3 py-1 rounded-md border border-gray-200 dark:border-gray-700 shadow-sm text-xs font-mono">
                <button class="fork-side-btn hover:text-emerald-500 transition-colors flex items-center gap-1 py-0.5 px-1.5 rounded hover:bg-gray-100 dark:hover:bg-white/5">
                    <span class="text-emerald-500 font-bold">+</span> Side Chat
                </button>
                <span class="text-gray-300 dark:text-gray-700">|</span>
                <button class="summary-btn hover:text-amber-500 transition-colors flex items-center gap-1 py-0.5 px-1.5 rounded hover:bg-gray-100 dark:hover:bg-white/5">
                    <span class="text-amber-500 font-bold">⚡</span> Summary
                </button>
            </div>
        </div>
    `;

    // 绑定 Fork Side 事件
    notebookBar.querySelector('.fork-side-btn').addEventListener('click', async () => {
        try {
            const res = await fetch('/api/fork-side', { method: 'POST' });
            if (res.ok) {
                const notice = document.createElement('div');
                notice.className = 'text-center my-3 text-xs text-emerald-600 dark:text-emerald-400 font-mono bg-emerald-50 dark:bg-emerald-950/40 py-1 rounded-lg border border-emerald-200 dark:border-emerald-800/50';
                notice.innerText = '🌿 已切换至侧线分支 (Side Chat)';
                chatBox.appendChild(notice);
                chatBox.scrollTop = chatBox.scrollHeight;
            }
        } catch (e) {
            console.error('Fork Side 失败:', e);
        }
    });

    // 绑定 Summary 事件
    notebookBar.querySelector('.summary-btn').addEventListener('click', async () => {
        const btn = notebookBar.querySelector('.summary-btn');

        // 1. 如果已经存在总结框，再次点击可以切换展开/隐藏
        let summaryBox = notebookBar.parentElement?.querySelector('.summary-box');
        if (summaryBox) {
            summaryBox.classList.toggle('hidden');
            return;
        }

        // 2. 创建黄色虚线框容器
        summaryBox = document.createElement('div');
        summaryBox.className = 'summary-box w-full mt-3 p-3.5 border-2 border-dashed border-amber-400/80 dark:border-amber-500/70 bg-amber-50/40 dark:bg-amber-950/20 rounded-xl text-xs text-gray-700 dark:text-gray-200 font-sans shadow-sm transition-all';
        summaryBox.innerHTML = `
            <div class="flex items-center gap-2 text-amber-600 dark:text-amber-400 font-mono font-medium mb-1.5">
                <span>⚡</span> 对话摘要
            </div>
            <div class="summary-content markdown-body text-xs opacity-90">正在生成总结...</div>
        `;

        if (notebookBar.parentElement) {
            notebookBar.parentElement.appendChild(summaryBox);
        } else {
            notebookBar.appendChild(summaryBox);
        }

        const contentDiv = summaryBox.querySelector('.summary-content');
        const customUrl = localStorage.getItem('custom_api_url') || '';
        const customKey = localStorage.getItem('custom_api_key') || '';

        try {
            btn.innerHTML = `<span class="text-amber-500 animate-spin">⏳</span> Summarizing...`;

            const res = await fetch('/api/generate-abstract', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ custom_url: customUrl, custom_key: customKey })
            });

            if (!res.ok) throw new Error("生成总结失败");

            // 同步直接解析 JSON
            const data = await res.json();

            // 假设 Go 后端同步返回格式为 {"abstract": "摘要内容"} 或 {"DialogAbstract": "摘要内容"}
            const textResult = data.abstract || data.DialogAbstract || "暂无摘要内容";

            contentDiv.innerHTML = safeMarkdownParse(textResult);
            btn.innerHTML = `<span class="text-amber-500 font-bold">✓</span> Summary`;

        } catch (e) {
            console.error('Summary 失败:', e);
            contentDiv.innerHTML = `<span class="text-rose-500">生成总结时出现错误：${e.message}</span>`;
            btn.innerHTML = `<span class="text-amber-500 font-bold">⚡</span> Summary`;
        }
    });

    return notebookBar;
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
async function downloadChat() {
    try {
        // 从后端获取真实的原始 Markdown 对话历史
        const res = await fetch('/api/history');
        const data = await res.json();

        if (!data || data.length === 0) {
            alert("暂无聊天记录可导出");
            return;
        }

        let content = "--- 聊天记录 ---\n\n";
        data.forEach(m => {
            if (m.role === 'system') return; // 忽略系统提示词，只导出用户和 AI 的对话

            const role = m.role === 'user' ? "【用户】" : "【AI】";
            content += `${role}\n${m.content.trim()}\n\n`;
        });

        const blob = new Blob([content], { type: 'text/plain;charset=utf-8' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `chat-${Date.now()}.txt`;
        a.click();
        URL.revokeObjectURL(url);
    } catch (e) {
        console.error("导出聊天记录失败:", e);
        alert("导出失败，请重试");
    }
}

async function newChat() {
    if (confirm("清空所有对话？")) {
        // 如果当前正在生成回复，强行打断请求
        if (chatAbortController) {
            chatAbortController.abort();
            chatAbortController = null;
        }

        // 还原按钮与加载状态
        loading.classList.add('hidden');
        sendBtn.classList.remove('is-loading');
        sendBtn.innerHTML = '发送';

        try {
            await fetch('/api/new-chat');
            chatBox.innerHTML = `
                <div class="flex justify-start mb-8">
                    <div class="ai-bubble p-4 rounded-2xl max-w-[90%] markdown-body">
                        你好!
                    </div>
                </div>`;
            get_ctx_usage(); // 刷新的同时重置 Context 计算
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

async function checkLlamaConnection() {
    const customUrl = localStorage.getItem('custom_api_url') || '';
    let healthUrl = '/health';

    if (customUrl) {
        try {
            const urlObj = new URL(customUrl);
            healthUrl = `${urlObj.protocol}//${urlObj.host}/health`;
        } catch (e) {
            console.error("解析自定义 URL 失败:", e);
        }
    }

    try {
        const controller = new AbortController();
        const timeoutId = setTimeout(() => controller.abort(), 3000);

        const res = await fetch(healthUrl, {
            method: 'GET',
            signal: controller.signal
        });

        clearTimeout(timeoutId);

        if (res.ok) {
            const data = await res.json();
            if (data.status === 'ok') {
                statusDot.className = "w-2.5 h-2.5 rounded-full bg-emerald-500 transition-colors duration-300 shadow-[0_0_8px_rgba(16,185,129,0.5)]";
                statusDot.title = "已成功连接到 llama.cpp";
                return;
            }
        }
        throw new Error("服务状态异常");

    } catch (err) {
        statusDot.className = "w-2.5 h-2.5 rounded-full bg-rose-500 transition-colors duration-300 shadow-[0_0_8px_rgba(244,63,94,0.5)] animate-pulse";
        statusDot.title = "无法连接到 llama.cpp，请检查后端服务是否启动";
    }
}

// 获取并更新当前 llama.cpp 的上下文 Token 使用状态
async function get_ctx_usage() {
    const customUrl = localStorage.getItem('custom_api_url') || '';
    const tokenDisplay = document.getElementById('token-usage');
    if (!tokenDisplay) return;

    try {
        const propsUrl = `/api/llama-props?custom_url=${encodeURIComponent(customUrl)}`;
        const res = await fetch(propsUrl);

        if (res.ok) {
            const data = await res.json();
            const maxCtx = data.default_generation_settings?.n_ctx || 0;
            let currentCtx = 0;

            if (data.slots && data.slots.length > 0) {
                currentCtx = data.slots[0].n_past || 0;
            }

            // 拿不到有效 maxCtx 时，直接优雅回退
            if (maxCtx <= 0) {
                tokenDisplay.innerText = `-- / -- (0%)`;
                tokenDisplay.classList.remove('text-rose-500');
                return;
            }

            const percentage = Math.min(100, Math.round((currentCtx / maxCtx) * 100));
            tokenDisplay.innerText = `${currentCtx} tokens / ${maxCtx} tokens (${percentage}%)`;

            if (percentage > 85) {
                tokenDisplay.classList.add('text-rose-500');
            } else {
                tokenDisplay.classList.remove('text-rose-500');
            }
        }
    } catch (err) {
        console.error("获取 Context 失败:", err);
        tokenDisplay.innerText = `-- / -- (0%)`;
        tokenDisplay.classList.remove('text-rose-500');
    }
}
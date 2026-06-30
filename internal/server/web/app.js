// ============================================================
// Share — app.js
// WebCrypto device-auth + file browser client
// ============================================================

// ============================================================
// SECTION 1 — Auth State & Storage Keys
// ============================================================

const STORAGE_KEY_PRIV = 'share_privkey_jwk';
const STORAGE_KEY_PUB  = 'share_pubkey_b64';
const STORAGE_KEY_NAME = 'share_device_name';

// Auth flow state
let myPublicKeyB64  = null;  // base64 SPKI public key
let myPrivateCrypto = null;  // CryptoKey (non-extractable private key)
let myDeviceName    = null;

// ============================================================
// SECTION 1b — Safe localStorage wrapper
//
// Safari in Private Browsing mode throws a SecurityError on any
// localStorage access. Wrap every read/write so we degrade gracefully
// rather than crashing the auth flow silently.
// ============================================================

const store = (() => {
    let _available = null;
    function available() {
        if (_available !== null) return _available;
        try {
            localStorage.setItem('__share_test__', '1');
            localStorage.removeItem('__share_test__');
            _available = true;
        } catch (e) {
            _available = false;
        }
        return _available;
    }
    return {
        get(key) {
            if (!available()) return null;
            try { return localStorage.getItem(key); } catch (e) { return null; }
        },
        set(key, value) {
            if (!available()) return;
            try { localStorage.setItem(key, value); } catch (e) { /* ignore */ }
        },
        remove(key) {
            if (!available()) return;
            try { localStorage.removeItem(key); } catch (e) { /* ignore */ }
        },
        isAvailable() { return available(); },
    };
})();

// ============================================================
// SECTION 2 — Secure Context Detection
// ============================================================

/**
 * crypto.subtle is only available in secure contexts (HTTPS or localhost).
 * On plain HTTP over a LAN IP, the browser blocks it entirely.
 * We detect this upfront and use a simpler token-based flow as fallback.
 */
const IS_SECURE_CONTEXT = !!(
    window.isSecureContext &&
    typeof crypto !== 'undefined' &&
    typeof crypto.subtle !== 'undefined'
);

// ============================================================
// SECTION 3 — Crypto Helpers (WebCrypto API — secure contexts only)
// ============================================================

/** Generate an ECDSA P-256 key pair and persist it. */
async function generateAndStoreKeyPair() {
    const keyPair = await crypto.subtle.generateKey(
        { name: 'ECDSA', namedCurve: 'P-256' },
        true,  // extractable so we can serialise the private key to JWK
        ['sign', 'verify']
    );

    // Export private key as JWK for persistent storage
    const privJwk = await crypto.subtle.exportKey('jwk', keyPair.privateKey);
    // Export public key as SPKI (the format the Go server expects)
    const pubSpki  = await crypto.subtle.exportKey('spki', keyPair.publicKey);
    const pubB64   = arrayBufferToBase64(pubSpki);

    store.set(STORAGE_KEY_PRIV, JSON.stringify(privJwk));
    store.set(STORAGE_KEY_PUB,  pubB64);

    myPublicKeyB64  = pubB64;
    myPrivateCrypto = keyPair.privateKey;
    return pubB64;
}

/** Load an existing key pair from LocalStorage, re-import the private key. */
async function loadStoredKeyPair() {
    const privJwkStr = store.get(STORAGE_KEY_PRIV);
    const pubB64     = store.get(STORAGE_KEY_PUB);
    if (!privJwkStr || !pubB64) return false;

    try {
        const privJwk = JSON.parse(privJwkStr);
        myPrivateCrypto = await crypto.subtle.importKey(
            'jwk',
            privJwk,
            { name: 'ECDSA', namedCurve: 'P-256' },
            false,   // not extractable after re-import
            ['sign']
        );
        myPublicKeyB64 = pubB64;
        myDeviceName   = store.get(STORAGE_KEY_NAME) || null;
        return true;
    } catch (e) {
        console.error('Failed to re-import stored key:', e);
        return false;
    }
}

/**
 * Sign a challenge nonce with the stored private key.
 * Returns a raw 64-byte (r‖s) signature as base64, matching the Go verifier.
 */
async function signChallenge(nonce) {
    const encoder   = new TextEncoder();
    const data      = encoder.encode(nonce);
    const sigBuffer = await crypto.subtle.sign(
        { name: 'ECDSA', hash: { name: 'SHA-256' } },
        myPrivateCrypto,
        data
    );
    return arrayBufferToBase64(sigBuffer);
}

// ============================================================
// SECTION 4 — Simple (Insecure Context) Device ID
// ============================================================

/**
 * When crypto.subtle is unavailable (plain HTTP over LAN), we generate a
 * random hex string as a device identifier instead of a real key pair.
 * The TUI approval step still acts as the trust gate.
 */
function generateSimpleDeviceId() {
    const array = new Uint8Array(24);
    crypto.getRandomValues(array); // crypto.getRandomValues works in all contexts
    return 'simple-' + Array.from(array).map(b => b.toString(16).padStart(2, '0')).join('');
}

function loadOrCreateSimpleDeviceId() {
    let id = store.get(STORAGE_KEY_PUB);
    if (!id || !id.startsWith('simple-')) {
        id = generateSimpleDeviceId();
        store.set(STORAGE_KEY_PUB, id);
    }
    myPublicKeyB64 = id;
    myDeviceName   = store.get(STORAGE_KEY_NAME) || null;
    return id;
}

// ============================================================
// SECTION 3 — Utility Helpers
// ============================================================

function arrayBufferToBase64(buffer) {
    const bytes  = new Uint8Array(buffer);
    let binary   = '';
    for (let i = 0; i < bytes.byteLength; i++) binary += String.fromCharCode(bytes[i]);
    return btoa(binary);
}

function sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

// ============================================================
// SECTION 5 — Auth Flow Orchestration
// ============================================================

/** Show/hide the three screens */
function showScreen(name) {
    document.getElementById('auth-register-step').classList.toggle('hidden', name !== 'register');
    document.getElementById('auth-status-step').classList.toggle('hidden', name !== 'status');
    const mainApp = document.getElementById('main-app');
    if (name === 'app') {
        mainApp.style.display = '';
    } else {
        mainApp.style.display = 'none';
    }
}

function setStatusBadge(type, text) {
    const badge = document.getElementById('auth-status-badge');
    badge.className = `status-badge ${type}`;
    document.getElementById('auth-status-text').textContent = text;
}

/**
 * Main auth entry point — branches on whether crypto.subtle is available.
 */
async function runAuthFlow() {
    // Guard: localStorage unavailable (Safari Private Browsing)
    if (!store.isAvailable()) {
        showScreen('register');
        const card = document.querySelector('#auth-register-step .auth-card');
        card.innerHTML = `
            <div class="brand-icon" style="background:linear-gradient(135deg,#f59e0b,#ef4444)">
                <svg xmlns="http://www.w3.org/2000/svg" width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="white" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
            </div>
            <h2>Private Browsing Detected</h2>
            <p>Share requires <strong>localStorage</strong> to store your device credentials. Safari blocks this in Private Browsing mode.</p>
            <p>Please open this page in a <strong>regular (non-private) browser tab</strong> to continue.</p>`;
        return;
    }

    // Step 0: check if we already have a valid session cookie (e.g. server
    // restarted but the device is already approved). If the cookie is gone or
    // expired we still need to re-authenticate, but we do it silently without
    // showing the register screen.
    const sessionOk = await checkExistingSession();
    if (sessionOk) {
        showScreen('app');
        initApp();
        return;
    }

    if (IS_SECURE_CONTEXT) {
        await runSecureAuthFlow();
    } else {
        await runSimpleAuthFlow();
    }
}

/** Probe a protected endpoint to see if the current session cookie is still valid. */
async function checkExistingSession() {
    try {
        const res = await fetch('/api/stats', { credentials: 'same-origin' });
        return res.ok;
    } catch (e) {
        return false;
    }
}

// ── Secure flow (HTTPS / localhost) ──────────────────────────────────────────

async function runSecureAuthFlow() {
    const hasKeys = await loadStoredKeyPair();
    if (!hasKeys) {
        // Genuinely first visit on this browser — show the name prompt
        showScreen('register');
        return;
    }

    // Keys exist in localStorage. Check server-side status.
    try {
        const statusRes = await fetch(`/api/auth/status?pubkey=${encodeURIComponent(myPublicKeyB64)}`);
        if (statusRes.ok) {
            const { status } = await statusRes.json();

            if (status === 'approved') {
                // Silently re-authenticate (server restart, expired cookie, etc.)
                await doVerifyAndUnlock();
                return;
            }
            if (status === 'pending') {
                showScreen('status');
                document.getElementById('pending-device-name').textContent = myDeviceName || '—';
                await pollUntilApproved();
                return;
            }
            if (status === 'unknown') {
                // Server doesn't know this key yet (e.g. devices file was wiped,
                // or this is a different server instance). Re-register silently
                // using the stored name if we have one, otherwise prompt.
                if (myDeviceName) {
                    await registerDevice(myDeviceName);
                } else {
                    showScreen('register');
                }
                return;
            }
        }
    } catch (e) { /* network error — fall through */ }

    showScreen('register');
}

// ── Simple flow (plain HTTP — crypto.subtle unavailable) ─────────────────────

async function runSimpleAuthFlow() {
    const id = loadOrCreateSimpleDeviceId();

    try {
        const statusRes = await fetch(`/api/auth/status?pubkey=${encodeURIComponent(id)}`);
        if (statusRes.ok) {
            const { status } = await statusRes.json();

            if (status === 'approved') {
                // Silently re-authenticate
                await doSimpleVerifyAndUnlock();
                return;
            }
            if (status === 'pending') {
                showScreen('status');
                document.getElementById('pending-device-name').textContent = myDeviceName || '—';
                await pollUntilApproved(true);
                return;
            }
            if (status === 'unknown') {
                // Re-register silently if we have a stored name
                if (myDeviceName) {
                    await registerDevice(myDeviceName);
                } else {
                    showScreen('register');
                }
                return;
            }
        }
    } catch (e) { /* fall through */ }

    showScreen('register');
}

/** Register this device and move to the pending screen. */
async function registerDevice(deviceName) {
    if (IS_SECURE_CONTEXT) {
        await generateAndStoreKeyPair();
    } else {
        loadOrCreateSimpleDeviceId();
    }

    store.set(STORAGE_KEY_NAME, deviceName);
    myDeviceName = deviceName;

    const res = await fetch('/api/auth/register', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: deviceName, publicKey: myPublicKeyB64 }),
    });

    if (!res.ok) throw new Error(`Registration failed: ${res.status}`);

    const { status } = await res.json();
    if (status === 'approved') {
        IS_SECURE_CONTEXT ? await doVerifyAndUnlock() : await doSimpleVerifyAndUnlock();
    } else {
        showScreen('status');
        document.getElementById('pending-device-name').textContent = deviceName;
        setStatusBadge('pending', 'Pending authorization…');
        await pollUntilApproved(!IS_SECURE_CONTEXT);
    }
}

/** Poll /api/auth/status until approved or rejected. */
async function pollUntilApproved(simple = false) {
    const INTERVAL_MS = 2500;
    const MAX_POLLS   = 240;

    for (let i = 0; i < MAX_POLLS; i++) {
        await sleep(INTERVAL_MS);
        try {
            const res = await fetch(`/api/auth/status?pubkey=${encodeURIComponent(myPublicKeyB64)}`);
            if (!res.ok) continue;
            const { status } = await res.json();

            if (status === 'approved') {
                setStatusBadge('approved', 'Approved! Authenticating…');
                simple ? await doSimpleVerifyAndUnlock() : await doVerifyAndUnlock();
                return;
            }

            if (status === 'rejected' || status === 'unknown') {
                setStatusBadge('error', 'Request rejected. Refresh to try again.');
                store.remove(STORAGE_KEY_PRIV);
                store.remove(STORAGE_KEY_PUB);
                store.remove(STORAGE_KEY_NAME);
                return;
            }
        } catch (e) {
            console.warn('Poll error:', e);
        }
    }

    setStatusBadge('error', 'Timed out. Please refresh the page.');
}

/** Full ECDSA challenge-response (secure contexts). */
async function doVerifyAndUnlock() {
    try {
        const chalRes = await fetch('/api/auth/challenge');
        if (!chalRes.ok) throw new Error('Failed to fetch challenge');
        const { challenge } = await chalRes.json();

        const signature = await signChallenge(challenge);

        const verRes = await fetch('/api/auth/verify', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                publicKey: myPublicKeyB64,
                nonce:     challenge,
                signature: signature,
            }),
        });

        if (!verRes.ok) throw new Error(await verRes.text());

        showScreen('app');
        initApp();
    } catch (e) {
        console.error('Verify error:', e);
        setStatusBadge('error', `Auth error: ${e.message}`);
        showScreen('status');
    }
}

/** Simple session grant — no signature, just approved device ID (plain HTTP). */
async function doSimpleVerifyAndUnlock() {
    try {
        const res = await fetch('/api/auth/verify-simple', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ publicKey: myPublicKeyB64 }),
        });

        if (!res.ok) throw new Error(await res.text());

        showScreen('app');
        initApp();
    } catch (e) {
        console.error('Simple verify error:', e);
        setStatusBadge('error', `Auth error: ${e.message}`);
        showScreen('status');
    }
}

// ============================================================
// SECTION 5 — File Browser State & DOM References
// ============================================================

let currentPath = '';
let filesList   = [];
let viewMode    = 'grid';

// DOM refs — resolved after app is shown
let filesContainer, emptyState, breadcrumbs, searchInput;
let viewGridBtn, viewListBtn, serverAddress, sharingDir;
let dropZone, fileInput;
let uploadProgressContainer, uploadFilename, uploadPercentage, progressBarFill;
let qrBtn, qrModal, closeQrBtn, qrImage, modalServerUrl;

/** Wire up all DOM references (called once main-app is visible). */
function resolveDomRefs() {
    filesContainer           = document.getElementById('files-container');
    emptyState               = document.getElementById('empty-state');
    breadcrumbs              = document.getElementById('breadcrumbs');
    searchInput              = document.getElementById('search-input');
    viewGridBtn              = document.getElementById('view-grid');
    viewListBtn              = document.getElementById('view-list');
    serverAddress            = document.getElementById('server-address');
    sharingDir               = document.getElementById('sharing-dir');
    dropZone                 = document.getElementById('drop-zone');
    fileInput                = document.getElementById('file-input');
    uploadProgressContainer  = document.getElementById('upload-progress-container');
    uploadFilename           = document.getElementById('upload-filename');
    uploadPercentage         = document.getElementById('upload-percentage');
    progressBarFill          = document.getElementById('progress-bar-fill');
    qrBtn                    = document.getElementById('qr-btn');
    qrModal                  = document.getElementById('qr-modal');
    closeQrBtn               = document.getElementById('close-qr-btn');
    qrImage                  = document.getElementById('qr-image');
    modalServerUrl           = document.getElementById('modal-server-url');
}

/** Bootstrap the file-sharing app once authenticated. */
function initApp() {
    resolveDomRefs();

    const origin = window.location.origin;
    serverAddress.textContent = origin;
    modalServerUrl.textContent = origin;

    fetchDirectory('');
    fetchServerStats();
    setupEventListeners();
}

// ============================================================
// SECTION 6 — Bootstrap: Run on page load
// ============================================================

document.addEventListener('DOMContentLoaded', () => {
    // Hook up the registration form
    const registerBtn       = document.getElementById('register-btn');
    const deviceNameInput   = document.getElementById('device-name-input');

    registerBtn.addEventListener('click', async () => {
        const name = deviceNameInput.value.trim();
        if (!name) {
            deviceNameInput.focus();
            return;
        }
        registerBtn.disabled = true;
        registerBtn.textContent = 'Registering…';
        try {
            await registerDevice(name);
        } catch (e) {
            showToast(`Registration error: ${e.message}`, 'error');
            registerBtn.disabled = false;
            registerBtn.textContent = 'Request Access';
        }
    });

    deviceNameInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') registerBtn.click();
    });

    // Start the auth flow
    runAuthFlow();
});

// ============================================================
// SECTION 7 — Event Listeners
// ============================================================

function setupEventListeners() {
    viewGridBtn.addEventListener('click', () => setViewMode('grid'));
    viewListBtn.addEventListener('click', () => setViewMode('list'));
    searchInput.addEventListener('input', filterFiles);

    dropZone.addEventListener('click', () => fileInput.click());
    fileInput.addEventListener('change', handleFileSelect);

    ['dragenter', 'dragover'].forEach(evt => {
        dropZone.addEventListener(evt, (e) => { e.preventDefault(); dropZone.classList.add('dragover'); });
    });
    ['dragleave', 'drop'].forEach(evt => {
        dropZone.addEventListener(evt, (e) => { e.preventDefault(); dropZone.classList.remove('dragover'); });
    });
    dropZone.addEventListener('drop', handleDrop);

    qrBtn.addEventListener('click', openModal);
    closeQrBtn.addEventListener('click', closeModal);
    qrModal.addEventListener('click', (e) => { if (e.target === qrModal) closeModal(); });
}

// ============================================================
// SECTION 8 — Directory Fetching & Rendering
// ============================================================

async function fetchDirectory(path) {
    try {
        currentPath = path;
        updateBreadcrumbs();
        const res = await fetch(`/api/files?path=${encodeURIComponent(path)}`);
        if (res.status === 401) { runAuthFlow(); return; }
        if (!res.ok) throw new Error('Failed to fetch directory');
        const data = await res.json();
        filesList = data.entries || [];
        renderFiles();
    } catch (err) {
        showToast(err.message, 'error');
    }
}

async function fetchServerStats() {
    try {
        const res = await fetch('/api/stats');
        if (res.ok) {
            const data = await res.json();
            if (data.sharingDir) {
                sharingDir.textContent = data.sharingDir;
                sharingDir.title = data.sharingDir;
            }
        }
    } catch (e) { console.error('Stats error', e); }
}

// ============================================================
// SECTION 9 — File Icons & Types
// ============================================================

const Icons = {
    folder: `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>`,
    pdf:    `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><path d="M16 13a2 2 0 0 0-2-2H9v6h2a2 2 0 0 0 2-2v-2z"/></svg>`,
    image:  `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="8.5" cy="8.5" r="1.5"/><polyline points="21 15 16 10 5 21"/></svg>`,
    video:  `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="23 7 16 12 23 17 23 7"/><rect x="1" y="5" width="15" height="14" rx="2"/></svg>`,
    audio:  `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/></svg>`,
    code:   `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>`,
    file:   `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>`,
};

function getFileType(filename, isDir) {
    if (isDir) return 'folder';
    const ext = filename.split('.').pop().toLowerCase();
    if (ext === 'pdf') return 'pdf';
    if (['jpg','jpeg','png','gif','webp','svg','bmp','tiff'].includes(ext)) return 'image';
    if (['mp4','webm','mkv','mov','avi','wmv'].includes(ext)) return 'video';
    if (['mp3','wav','ogg','flac','m4a','aac'].includes(ext)) return 'audio';
    if (['go','py','js','css','html','json','sh','yml','yaml','md','c','cpp','rs','ts','java'].includes(ext)) return 'code';
    return 'file';
}

// ============================================================
// SECTION 10 — Render Files
// ============================================================

function renderFiles(filteredFiles = null) {
    filesContainer.innerHTML = '';
    const filesToRender = filteredFiles || filesList;

    if (filesToRender.length === 0) {
        filesContainer.style.display = 'none';
        emptyState.style.display = 'flex';
        return;
    }

    filesContainer.style.display = viewMode === 'grid' ? 'grid' : 'flex';
    emptyState.style.display = 'none';

    const sorted = [...filesToRender].sort((a, b) => {
        if (a.isDir && !b.isDir) return -1;
        if (!a.isDir && b.isDir) return 1;
        return a.name.localeCompare(b.name);
    });

    sorted.forEach(entry => {
        const type         = getFileType(entry.name, entry.isDir);
        const card         = document.createElement('div');
        card.className     = 'file-card';
        card.setAttribute('data-type', type);
        const sizeFormatted = entry.isDir ? '' : formatBytes(entry.size);
        const relativePath  = currentPath ? `${currentPath}/${entry.name}` : entry.name;

        card.innerHTML = `
            <div class="file-icon">${Icons[type]}</div>
            <div class="file-name" title="${entry.name}">${entry.name}</div>
            <div class="file-meta">${entry.isDir ? 'Folder' : sizeFormatted}</div>
            <div class="file-actions">
                ${entry.isDir ? '' : `
                <button class="action-btn download-btn" title="Download">
                    <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
                </button>`}
            </div>`;

        card.addEventListener('click', (e) => {
            if (e.target.closest('.action-btn')) return;
            if (entry.isDir) fetchDirectory(relativePath);
            else downloadFile(relativePath);
        });

        if (!entry.isDir) {
            card.querySelector('.download-btn').addEventListener('click', (e) => {
                e.stopPropagation();
                downloadFile(relativePath);
            });
        }

        filesContainer.appendChild(card);
    });
}

function updateBreadcrumbs() {
    breadcrumbs.innerHTML = '';
    const homeCrumb = document.createElement('a');
    homeCrumb.href = '#';
    homeCrumb.className = 'breadcrumb-item home-breadcrumb';
    homeCrumb.innerHTML = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="margin-right:4px"><path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/><polyline points="9 22 9 12 15 12 15 22"/></svg> Home`;
    homeCrumb.addEventListener('click', (e) => { e.preventDefault(); fetchDirectory(''); });
    breadcrumbs.appendChild(homeCrumb);

    if (!currentPath) return;

    const segments = currentPath.split('/');
    let cumulativePath = '';
    segments.forEach((segment, index) => {
        cumulativePath = cumulativePath ? `${cumulativePath}/${segment}` : segment;
        const sep = document.createElement('span');
        sep.className = 'breadcrumb-separator';
        sep.textContent = ' / ';
        breadcrumbs.appendChild(sep);

        if (index === segments.length - 1) {
            const cur = document.createElement('span');
            cur.className = 'breadcrumb-current';
            cur.textContent = segment;
            breadcrumbs.appendChild(cur);
        } else {
            const link = document.createElement('a');
            link.href = '#';
            link.className = 'breadcrumb-item';
            link.textContent = segment;
            const target = cumulativePath;
            link.addEventListener('click', (e) => { e.preventDefault(); fetchDirectory(target); });
            breadcrumbs.appendChild(link);
        }
    });
}

function downloadFile(relativePath) {
    window.location.href = `/files/${encodeURIComponent(relativePath)}`;
}

function setViewMode(mode) {
    viewMode = mode;
    viewGridBtn.classList.toggle('active', mode === 'grid');
    viewListBtn.classList.toggle('active', mode === 'list');
    filesContainer.classList.toggle('grid-layout', mode === 'grid');
    filesContainer.classList.toggle('list-layout', mode === 'list');
    renderFiles();
}

function filterFiles() {
    const query = searchInput.value.toLowerCase().trim();
    if (!query) { renderFiles(); return; }
    renderFiles(filesList.filter(f => f.name.toLowerCase().includes(query)));
}

// ============================================================
// SECTION 11 — Upload Handlers
// ============================================================

function handleDrop(e) { handleFiles(e.dataTransfer.files); }
function handleFileSelect(e) { handleFiles(e.target.files); }

function handleFiles(files) {
    if (!files.length) return;
    Array.from(files).forEach(uploadFile);
}

function uploadFile(file) {
    uploadProgressContainer.style.display = 'block';
    uploadFilename.textContent = file.name;
    uploadPercentage.textContent = '0%';
    progressBarFill.style.width = '0%';

    const xhr      = new XMLHttpRequest();
    const formData = new FormData();
    formData.append('file', file);

    xhr.upload.addEventListener('progress', (e) => {
        if (e.lengthComputable) {
            const pct = Math.round((e.loaded / e.total) * 100);
            uploadPercentage.textContent = `${pct}%`;
            progressBarFill.style.width  = `${pct}%`;
        }
    });

    xhr.addEventListener('load', () => {
        if (xhr.status === 200) {
            showToast(`"${file.name}" uploaded successfully!`, 'success');
            fetchDirectory(currentPath);
        } else if (xhr.status === 401) {
            showToast('Session expired. Re-authenticating…', 'error');
            runAuthFlow();
        } else {
            showToast(`Upload failed (${xhr.status})`, 'error');
        }
        setTimeout(() => { uploadProgressContainer.style.display = 'none'; }, 1500);
    });

    xhr.addEventListener('error', () => {
        showToast('Network error during upload', 'error');
        uploadProgressContainer.style.display = 'none';
    });

    xhr.open('POST', `/api/upload?path=${encodeURIComponent(currentPath)}`, true);
    xhr.send(formData);
}

// ============================================================
// SECTION 12 — QR Modal
// ============================================================

function openModal() {
    qrModal.style.display = 'flex';
    const encodedUrl = encodeURIComponent(window.location.origin);
    qrImage.src = `/api/qr?data=${encodedUrl}`;
}

function closeModal() {
    qrModal.style.display = 'none';
}

// ============================================================
// SECTION 13 — Toast Notifications
// ============================================================

function showToast(message, type = 'info') {
    const container = document.getElementById('toast-container');
    const toast     = document.createElement('div');
    toast.className = `toast ${type}`;

    const icons = {
        success: `<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>`,
        error:   `<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>`,
        info:    `<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>`,
    };

    toast.innerHTML = `${icons[type] || icons.info}<span>${message}</span>`;
    container.appendChild(toast);

    setTimeout(() => {
        toast.style.animation = 'slideInRight var(--transition-normal) reverse forwards';
        toast.addEventListener('animationend', () => toast.remove());
    }, 4000);
}

// ============================================================
// SECTION 14 — Formatters
// ============================================================

function formatBytes(bytes, decimals = 2) {
    if (bytes === 0) return '0 Bytes';
    const k     = 1024;
    const dm    = Math.max(0, decimals);
    const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
    const i     = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
}

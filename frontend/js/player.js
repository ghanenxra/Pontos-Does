document.addEventListener('DOMContentLoaded', () => {
    // Current Active Watch Session State
    let currentSessionId = null;
    let pingIntervalId = null;
    let currentVideoId = null;
    let currentUser = null;

    // Load active user details
    fetch('/api/me')
        .then(res => {
            if (!res.ok) throw new Error('Unauthenticated');
            return res.json();
        })
        .then(user => {
            currentUser = user;
            const profileHeader = document.getElementById('user-profile-header');
            if (profileHeader) {
                profileHeader.innerHTML = `
                    <span style="font-weight: 500; font-size: 0.9rem; color: var(--text-secondary);">${user.name}</span>
                    <img src="${user.avatar_url || 'https://www.gravatar.com/avatar?d=mp'}" class="user-avatar" alt="Avatar">
                `;
            }
        })
        .catch(() => {
            window.location.href = '/';
        });

    // Initialize Video.js Player
    const player = videojs('streamvault-player', {
        fluid: true,
        playbackRates: [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2],
        controlBar: {
            pictureInPictureToggle: true,
            volumePanel: { inline: false },
        }
    });

    // Custom Quality Selector in Video.js control bar
    const Button = videojs.getComponent('MenuButton');
    const MenuItem = videojs.getComponent('MenuItem');

    const QualityMenuItem = videojs.extend(MenuItem, {
        constructor: function(player, options) {
            MenuItem.call(this, player, options);
            this.label = options.label;
        },
        handleClick: function() {
            // Update UI/State
            this.player().trigger('qualitySelected', this.label);
        }
    });

    const QualityButton = videojs.extend(Button, {
        constructor: function(player, options) {
            Button.call(this, player, options);
            this.controlText('Quality');
        },
        createItems: function() {
            const qualities = ['Auto (Original)', '1080p', '720p', '485p'];
            return qualities.map(q => new QualityMenuItem(this.player(), {
                label: q,
                selectable: true,
                selected: q === 'Auto (Original)'
            }));
        },
        buildCSSClass: function() {
            return 'vjs-icon-cog ' + Button.prototype.buildCSSClass.call(this);
        }
    });

    videojs.registerComponent('QualityButton', QualityButton);
    player.ready(() => {
        player.controlBar.addChild('QualityButton', {}, player.controlBar.children_.length - 2);
    });

    player.on('qualitySelected', (e, quality) => {
        console.log('Selected Quality:', quality);
        // Direct stream or terabox bypasses multiple transcode tracks, show cosmetic alert/log
        videojs.log('Stream quality adjusted to ' + quality);
    });

    // Handle Tab switching
    const tabs = document.querySelectorAll('.tab-btn');
    const tabContents = document.querySelectorAll('.tab-content');

    tabs.forEach(tab => {
        tab.addEventListener('click', () => {
            tabs.forEach(t => t.classList.remove('active'));
            tabContents.forEach(c => c.classList.remove('active'));

            tab.classList.add('active');
            const target = document.getElementById(`tab-${tab.dataset.tab}`);
            if (target) target.classList.add('active');
        });
    });

    // Google Picker & GDrive Folder Navigation
    let pickerApiLoaded = false;
    let configSettings = null;

    // Fetch Google Client ID & Developer Key from backend
    fetch('/api/config')
        .then(res => res.json())
        .then(cfg => {
            configSettings = cfg;
            gapi.load('picker', { 'callback': () => { pickerApiLoaded = true; } });
        });

    const pickerBtn = document.getElementById('picker-btn');
    pickerBtn.addEventListener('click', () => {
        if (!pickerApiLoaded || !configSettings) {
            alert('Google Picker API loading. Please retry in a moment.');
            return;
        }

        // Securely fetch short-lived GDrive access token from database
        fetch('/api/drive/token')
            .then(res => {
                if (!res.ok) throw new Error('Could not fetch drive token');
                return res.json();
            })
            .then(data => {
                openGooglePicker(data.token);
            })
            .catch(err => {
                alert('Authentication failed: ' + err.message);
            });
    });

    function openGooglePicker(oauthToken) {
        const view = new google.picker.View(google.picker.ViewId.FOLDERS);
        
        const picker = new google.picker.PickerBuilder()
            .addView(view)
            .setOAuthToken(oauthToken)
            .setDeveloperKey(configSettings.developerKey)
            .setCallback((data) => {
                if (data[google.picker.Response.ACTION] === google.picker.Action.PICKED) {
                    const doc = data[google.picker.Response.DOCUMENTS][0];
                    const folderId = doc[google.picker.Document.ID];
                    const folderName = doc[google.picker.Document.NAME];
                    
                    displayFolder(folderId, folderName);
                }
            })
            .build();
        picker.setVisible(true);
    }

    function displayFolder(folderId, folderName) {
        const infoDiv = document.getElementById('folder-info');
        const nameSpan = document.getElementById('folder-name');
        nameSpan.textContent = folderName;
        infoDiv.style.display = 'flex';

        loadDriveFiles(folderId);
    }

    // List files and directories within a Google Drive folder
    function loadDriveFiles(folderId) {
        const container = document.getElementById('file-list-container');
        container.innerHTML = `<div style="padding: 1.5rem; text-align: center; color: var(--text-muted);">Loading folder items...</div>`;

        fetch(`/api/drive/files?folderId=${folderId}`)
            .then(res => {
                if (!res.ok) throw new Error('Failed to retrieve files');
                return res.json();
            })
            .then(data => {
                container.innerHTML = '';
                const files = data.files || [];

                if (files.length === 0) {
                    container.innerHTML = `<div style="padding: 1.5rem; text-align: center; color: var(--text-muted); font-size: 0.85rem;">This folder is empty.</div>`;
                    return;
                }

                // Render items
                files.forEach(file => {
                    const isFolder = file.mimeType === 'application/vnd.google-apps.folder';
                    const div = document.createElement('div');
                    div.className = `file-item ${isFolder ? 'folder' : 'video'}`;
                    
                    const icon = isFolder 
                        ? `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-right: 6px;"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path></svg>`
                        : `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-right: 6px;"><polygon points="5 3 19 12 5 21 5 3"></polygon></svg>`;
                    
                    div.innerHTML = `${icon} <span style="overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">${file.name}</span>`;
                    
                    div.addEventListener('click', () => {
                        if (isFolder) {
                            displayFolder(file.id, file.name);
                        } else {
                            loadVideo('gdrive', file.id, file.name, file.thumbnailLink, folderId);
                        }
                    });
                    
                    container.appendChild(div);
                });
            })
            .catch(err => {
                container.innerHTML = `<div style="padding: 1.5rem; text-align: center; color: #ef4444; font-size: 0.85rem;">Error loading items: ${err.message}</div>`;
            });
    }

    document.getElementById('clear-folder').addEventListener('click', (e) => {
        e.preventDefault();
        document.getElementById('folder-info').style.display = 'none';
        document.getElementById('file-list-container').innerHTML = `
            <div style="padding: 1.5rem; text-align: center; color: var(--text-muted); font-size: 0.85rem;">
                No folder selected yet.
            </div>
        `;
    });

    // Terabox Loading Input
    const loadTeraboxBtn = document.getElementById('load-terabox-btn');
    loadTeraboxBtn.addEventListener('click', () => {
        const urlInput = document.getElementById('terabox-url').value.trim();
        const titleInput = document.getElementById('terabox-title').value.trim();

        if (!urlInput || !titleInput) {
            alert('Please fill out all fields.');
            return;
        }

        loadVideo('terabox', urlInput, titleInput);
    });

    // Direct Stream Loading Input
    const loadDirectBtn = document.getElementById('load-direct-btn');
    loadDirectBtn.addEventListener('click', () => {
        const urlInput = document.getElementById('direct-url').value.trim();
        const titleInput = document.getElementById('direct-title').value.trim();

        if (!urlInput || !titleInput) {
            alert('Please fill out all fields.');
            return;
        }

        loadVideo('direct', urlInput, titleInput);
    });

    // Load URL or File ID into Video.js player, set subtitles and configure tracking session
    function loadVideo(sourceType, sourceId, title, thumbnail = '', folderId = '') {
        // Clear previous progress tracking
        endWatchSession();

        const videoTitleEl = document.getElementById('now-playing-title');
        videoTitleEl.textContent = title;

        let srcUrl = '';
        if (sourceType === 'gdrive') {
            srcUrl = `/api/stream?source=gdrive&fileId=${sourceId}`;
        } else if (sourceType === 'terabox') {
            srcUrl = `/api/stream?source=terabox&url=${encodeURIComponent(sourceId)}`;
        } else if (sourceType === 'direct') {
            srcUrl = `/api/stream?source=direct&url=${encodeURIComponent(sourceId)}`;
        }

        // Video.js load source
        player.src({
            src: srcUrl,
            type: 'video/mp4' // Fallback typical stream type, works with proxy byte stream
        });

        // Subtitles configuration (Google Drive folder search)
        // Clear any old text tracks
        const tracks = player.remoteTextTracks();
        let i = tracks.length;
        while (i--) {
            player.removeRemoteTextTrack(tracks[i]);
        }

        if (sourceType === 'gdrive' && folderId) {
            fetch(`/api/drive/subtitles?folderId=${folderId}`)
                .then(res => res.json())
                .then(subs => {
                    subs.forEach(sub => {
                        // Append tracks
                        player.addRemoteTextTrack({
                            kind: 'subtitles',
                            src: `/api/stream?source=gdrive&fileId=${sub.id}`,
                            srclang: 'en',
                            label: sub.name,
                            default: sub.name.toLowerCase().includes('eng')
                        }, true);
                    });
                })
                .catch(err => console.log('Error loading subtitles:', err));
        }

        // Start watch session in database
        startWatchSession(sourceType, sourceId, title, thumbnail);
    }

    // -------------------------------------------------------------
    // PROGRESS TRACKING MECHANISMS
    // -------------------------------------------------------------

    function startWatchSession(sourceType, sourceId, title, thumbnail) {
        // Wait until metadata is loaded to retrieve exact duration
        player.one('loadedmetadata', () => {
            const duration = Math.floor(player.duration()) || 0;

            const payload = {
                video_title: title,
                source_type: sourceType,
                source_id: sourceId,
                thumbnail_url: thumbnail || 'https://images.unsplash.com/photo-1536440136628-849c177e76a1?w=500&auto=format&fit=crop&q=60',
                duration_seconds: duration
            };

            fetch('/api/watch/start', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload)
            })
            .then(res => res.json())
            .then(data => {
                currentSessionId = data.session_id;
                currentVideoId = data.video_id;
                const resumePos = data.last_position_seconds || 0;

                if (resumePos > 0 && resumePos < (duration - 10)) {
                    const confirmResume = confirm(`Resume playback from last position: ${formatTime(resumePos)}?`);
                    if (confirmResume) {
                        player.currentTime(resumePos);
                    }
                }

                // Autoplay
                player.play().catch(() => {});

                // Start 10 seconds progress ping interval
                startPingInterval();
            })
            .catch(err => console.error('Failed to initialize watch session:', err));
        });
    }

    function startPingInterval() {
        if (pingIntervalId) clearInterval(pingIntervalId);

        pingIntervalId = setInterval(() => {
            // Only ping if player is currently active
            if (currentSessionId && !player.paused() && !player.ended()) {
                const pos = Math.floor(player.currentTime());
                
                fetch('/api/watch/ping', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        session_id: currentSessionId,
                        position_seconds: pos
                    })
                })
                .then(res => {
                    if (!res.ok) throw new Error('Ping failed');
                })
                .catch(err => console.warn('Progress ping issue:', err));
            }
        }, 10000); // 10s intervals
    }

    function endWatchSession() {
        if (pingIntervalId) {
            clearInterval(pingIntervalId);
            pingIntervalId = null;
        }

        if (currentSessionId) {
            const pos = Math.floor(player.currentTime());
            
            // Execute Beacon or standard fetch to register final position
            const payload = JSON.stringify({
                session_id: currentSessionId,
                position_seconds: pos
            });

            // If page is closing, use sendBeacon for guarantees
            navigator.sendBeacon('/api/watch/end', payload);
            
            currentSessionId = null;
        }
    }

    // Trigger end session on tab changes or unloads
    window.addEventListener('beforeunload', endWatchSession);
    player.on('ended', () => {
        endWatchSession();
    });

    // Helper: format position time
    function formatTime(secs) {
        const h = Math.floor(secs / 3600);
        const m = Math.floor((secs % 3600) / 60);
        const s = secs % 60;
        return [
            h > 0 ? h : null,
            h > 0 && m < 10 ? '0' + m : m,
            s < 10 ? '0' + s : s
        ].filter(x => x !== null).join(':');
    }

    // Check query params if loaded from History Resume trigger
    const urlParams = new URLSearchParams(window.location.search);
    const resumeSource = urlParams.get('source');
    const resumeId = urlParams.get('id');
    const resumeTitle = urlParams.get('title');

    if (resumeSource && resumeId && resumeTitle) {
        loadVideo(resumeSource, resumeId, resumeTitle);
    }
});

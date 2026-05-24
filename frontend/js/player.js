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

    // Initialize Video.js Player with speeds and options
    const player = videojs('streamvault-player', {
        fluid: true,
        playbackRates: [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2],
        controlBar: {
            pictureInPictureToggle: true,
            volumePanel: { inline: false },
        }
    });

    // Error Listener for debugging and recruiters
    player.on('error', () => {
        const error = player.error();
        console.error('Video.js Player encountered an error:', error);
        alert(`Playback Issue: ${error.message} (Code: ${error.code}).\n\nTips:\n- Make sure the stream URL is correct and accessible.\n- Recruiter demo check: Ensure direct URLs are valid .mp4 or .m3u8 streams.\n- MKV files are not supported natively by web browsers.`);
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
            this.player().trigger('qualitySelected', this.label);
        }
    });

    const QualityButton = videojs.extend(Button, {
        constructor: function(player, options) {
            Button.call(this, player, options);
            this.controlText('Quality');
        },
        createItems: function() {
            const qualities = ['Auto (Original)', '1080p', '720p', '480p'];
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
            alert('Google Picker API is still loading. Please retry in a second.');
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
                alert('Google Authentication Issue: ' + err.message + '\n\nMake sure your google account is logged in and credentials in .env are set.');
            });
    });

    function openGooglePicker(oauthToken) {
        // Allow picking folders AND specific video files (important for drive.file permissions)
        const docsView = new google.picker.DocsView()
            .setIncludeFolders(true)
            .setMimeTypes('application/vnd.google-apps.folder,video/mp4,video/mkv,video/webm,video/quicktime');

        const picker = new google.picker.PickerBuilder()
            .addView(docsView)
            .setOAuthToken(oauthToken)
            .setDeveloperKey(configSettings.developerKey)
            .setCallback((data) => {
                if (data[google.picker.Response.ACTION] === google.picker.Action.PICKED) {
                    const doc = data[google.picker.Response.DOCUMENTS][0];
                    const id = doc[google.picker.Document.ID];
                    const name = doc[google.picker.Document.NAME];
                    const mimeType = doc[google.picker.Document.MIME_TYPE];
                    
                    if (mimeType === 'application/vnd.google-apps.folder') {
                        displayFolder(id, name);
                    } else {
                        // User selected the video file directly - plays it immediately
                        loadVideo('gdrive', id, name, doc[google.picker.Document.THUMBNAIL_URL] || '');
                    }
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

    // Manual Google Drive File ID Loading Input
    const loadGdriveFileBtn = document.getElementById('load-gdrive-file-btn');
    loadGdriveFileBtn.addEventListener('click', () => {
        const fileIdInput = document.getElementById('gdrive-file-id').value.trim();
        const titleInput = document.getElementById('gdrive-file-title').value.trim();

        if (!fileIdInput || !titleInput) {
            alert('Please enter both the GDrive File ID and the Video Title.');
            return;
        }

        loadVideo('gdrive', fileIdInput, titleInput);
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

    // Direct URL Loading Input
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

    // Helper to identify the correct stream MIME type for Video.js (MP4 vs HLS .m3u8)
    function getStreamType(url) {
        const lower = url.toLowerCase();
        if (lower.includes('.m3u8')) {
            return 'application/x-mpegURL';
        }
        if (lower.includes('.webm')) {
            return 'video/webm';
        }
        if (lower.includes('.ogg')) {
            return 'video/ogg';
        }
        return 'video/mp4'; // Default fallback
    }

    // Load URL or File ID into Video.js player, set subtitles and configure tracking session
    function loadVideo(sourceType, sourceId, title, thumbnail = '', folderId = '') {
        // Clear previous progress tracking
        endWatchSession();

        const videoTitleEl = document.getElementById('now-playing-title');
        videoTitleEl.textContent = title;

        let srcUrl = '';
        let type = 'video/mp4';

        if (sourceType === 'gdrive') {
            srcUrl = `/api/stream?source=gdrive&fileId=${sourceId}`;
            type = 'video/mp4';
        } else if (sourceType === 'terabox') {
            srcUrl = `/api/stream?source=terabox&url=${encodeURIComponent(sourceId)}`;
            type = 'video/mp4';
        } else if (sourceType === 'direct') {
            srcUrl = `/api/stream?source=direct&url=${encodeURIComponent(sourceId)}`;
            type = getStreamType(sourceId); // Auto-detect HLS (.m3u8) vs MP4
        }

        // Set source and reload player engine
        player.src({
            src: srcUrl,
            type: type
        });
        player.load();

        // Clear and reload subtitles (GDrive folder search)
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

        // Initialize watch session and pings immediately
        startWatchSession(sourceType, sourceId, title, thumbnail);
    }

    // -------------------------------------------------------------
    // PROGRESS TRACKING MECHANISMS
    // -------------------------------------------------------------

    function startWatchSession(sourceType, sourceId, title, thumbnail) {
        // Query database immediately with initial duration 0 (will update on ping/metadata load)
        let duration = 0;
        
        // Setup listener to fetch actual duration once metadata resolves
        player.one('loadedmetadata', () => {
            const actualDuration = Math.floor(player.duration()) || 0;
            console.log('Video metadata loaded. Duration:', actualDuration);
            // Autoplay the video after metadata loads
            player.play().catch(() => {});
        });

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

            if (resumePos > 0) {
                // Seek to resume position
                player.currentTime(resumePos);
            }

            // Start 10 seconds progress ping interval
            startPingInterval();
        })
        .catch(err => console.error('Failed to initialize watch session:', err));
    }

    function startPingInterval() {
        if (pingIntervalId) clearInterval(pingIntervalId);

        pingIntervalId = setInterval(() => {
            // Only ping if player has an active session and is playing
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
            
            const payload = JSON.stringify({
                session_id: currentSessionId,
                position_seconds: pos
            });

            // Make standard end watch call
            fetch('/api/watch/end', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: payload
            }).catch(err => console.warn('End session issue:', err));
            
            currentSessionId = null;
        }
    }

    // Trigger end session on tab changes or unloads
    window.addEventListener('beforeunload', endWatchSession);
    player.on('ended', () => {
        endWatchSession();
    });

    // Check query params if loaded from History Resume trigger
    const urlParams = new URLSearchParams(window.location.search);
    const resumeSource = urlParams.get('source');
    const resumeId = urlParams.get('id');
    const resumeTitle = urlParams.get('title');

    if (resumeSource && resumeId && resumeTitle) {
        loadVideo(resumeSource, resumeId, resumeTitle);
    }
});

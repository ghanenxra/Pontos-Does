document.addEventListener('DOMContentLoaded', () => {
    // 1. Check user login status to adapt landing links
    fetch('/api/me')
        .then(res => {
            if (res.ok) {
                return res.json();
            }
            throw new Error('Unauthorized');
        })
        .then(user => {
            const header = document.querySelector('.landing-header');
            if (header) {
                header.innerHTML = `
                    <div style="display: flex; align-items: center; gap: 1.5rem;">
                        <a href="/history" class="nav-link">History</a>
                        <img src="${user.avatar_url || 'https://www.gravatar.com/avatar?d=mp'}" class="user-avatar" alt="Avatar">
                    </div>
                `;
            }
            const loginContainer = document.getElementById('login-container');
            if (loginContainer) {
                loginContainer.innerHTML = `
                    <a href="/player" class="google-signin-btn small-btn" style="background: var(--accent-purple); color: #0a0a0f; border-color: var(--accent-purple);">
                        Go to Player
                    </a>
                `;
            }
        })
        .catch(() => {
            // Keep default 'Sign in with Google' button if not logged in
        });

    // 2. Wave Canvas Animation
    const canvas = document.getElementById('wave-canvas');
    if (!canvas) return;
    const ctx = canvas.getContext('2d');

    let width = canvas.width = window.innerWidth;
    let height = canvas.height = window.innerHeight;

    window.addEventListener('resize', () => {
        width = canvas.width = window.innerWidth;
        height = canvas.height = window.innerHeight;
    });

    let phase = 0;
    const speed = 0.018; // Speed per frame
    const cycles = 1.5;  // Frequency cycles

    function animate() {
        ctx.clearRect(0, 0, width, height);

        // Amplitude is ~13% of container height
        const amplitude = height * 0.13;
        const centerY = height / 2;

        ctx.beginPath();

        // Standard gradient colors: purple -> indigo -> sky -> teal -> pink
        const gradient = ctx.createLinearGradient(0, 0, width, 0);
        gradient.addColorStop(0, '#a855f7'); // purple
        gradient.addColorStop(0.25, '#6366f1'); // indigo
        gradient.addColorStop(0.5, '#0ea5e9'); // sky
        gradient.addColorStop(0.75, '#14b8a6'); // teal
        gradient.addColorStop(1, '#ec4899'); // pink

        ctx.strokeStyle = gradient;
        ctx.lineWidth = 6;
        ctx.lineCap = 'round';
        ctx.shadowColor = 'rgba(168, 85, 247, 0.4)';
        ctx.shadowBlur = 20;

        const points = [];
        const step = 20; // Step size in pixels for plotting

        // Generate points for 1.5 cycles
        // Wavelength L: width / cycles
        const wavelength = width / cycles;
        for (let x = 0; x <= width + step; x += step) {
            const angle = (x / wavelength) * Math.PI * 2 + phase;
            const y = centerY + Math.sin(angle) * amplitude;
            points.push({ x, y });
        }

        // Draw smooth path using quadraticCurveTo path smoothing
        if (points.length > 0) {
            ctx.moveTo(points[0].x, points[0].y);
            
            for (let i = 0; i < points.length - 1; i++) {
                const xc = (points[i].x + points[i + 1].x) / 2;
                const yc = (points[i].y + points[i + 1].y) / 2;
                ctx.quadraticCurveTo(points[i].x, points[i].y, xc, yc);
            }
            
            ctx.lineTo(points[points.length - 1].x, points[points.length - 1].y);
        }

        ctx.stroke();

        // Increment phase
        phase += speed;

        requestAnimationFrame(animate);
    }

    animate();
});

import type { MockUser } from "./types.js";

export const renderUserSelectionPage = (users: MockUser[], queryString: string): string => {
  const cardsHtml = users
    .map(
      (user) => `
        <div class="user-card" data-email="${user.email}">
          <h3>${user.name}</h3>
          <p class="email">${user.email}</p>
        </div>
      `
    )
    .join("");

  return `
    <!DOCTYPE html>
    <html>
      <head>
        <meta charset="utf-8" />
        <title>Mock OAuth - Select User</title>
        <style>
          :root { color-scheme: dark; }
          body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial,
              'Noto Sans', 'Apple Color Emoji', 'Segoe UI Emoji';
            margin: 40px;
            background-color: #09090b;
            color: #fafafa;
          }
          .container { max-width: 720px; margin: 0 auto; }
          .muted { color: #a1a1aa; }
          .user-card {
            border: 1px solid #27272a;
            background: #111113;
            padding: 16px 20px;
            margin: 12px 0;
            border-radius: 8px;
            cursor: pointer;
            transition: background-color .15s ease, border-color .15s ease;
          }
          .user-card:hover { background-color: #18181b; border-color: #3f3f46; }
          h1 { font-size: 22px; margin: 0 0 6px; }
          p.sub { margin: 0 0 12px; }
          h3 { margin: 0 0 4px; font-size: 16px; }
          p.email { font-size: 13px; color: #a1a1aa; margin: 0; }
          .cancel-btn {
            display: inline-block;
            margin-top: 20px;
            padding: 10px 20px;
            background: #27272a;
            color: #a1a1aa;
            border: 1px solid #3f3f46;
            border-radius: 6px;
            cursor: pointer;
            text-decoration: none;
            font-size: 14px;
            transition: background-color .15s ease, border-color .15s ease;
          }
          .cancel-btn:hover { background: #3f3f46; color: #fafafa; }
        </style>
      </head>
      <body>
        <div class="container">
          <h1>Mock Google OAuth – Select User</h1>
          <p class="sub muted">Choose which user to authenticate as:</p>
          ${cardsHtml}
          <button class="cancel-btn" id="cancel-btn">Cancel</button>
        </div>
        <script>
          const params = new URLSearchParams(${JSON.stringify(queryString)});
          document.querySelectorAll('.user-card').forEach((card) => {
            card.addEventListener('click', () => {
              const email = card.getAttribute('data-email');
              if (!email) return;
              params.set('selected_user', email);
              sessionStorage.setItem('selectedUser', email);
              window.location.href = '/oauth2/v2/auth?' + params.toString();
            });
          });

          document.getElementById('cancel-btn')?.addEventListener('click', () => {
            const redirectUri = params.get('redirect_uri');
            const state = params.get('state');
            if (redirectUri) {
              const cancelUrl = new URL(redirectUri);
              cancelUrl.searchParams.set('error', 'access_denied');
              cancelUrl.searchParams.set('error_description', 'User cancelled the authorization request');
              if (state) {
                cancelUrl.searchParams.set('state', state);
              }
              window.location.href = cancelUrl.toString();
            }
          });
        </script>
      </body>
    </html>
  `;
};

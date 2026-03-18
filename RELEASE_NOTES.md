# Release Notes - v1.3.1 (Latest)

## ⚡ Build Optimization & Clean-up
- **CGO Disabled (Static Builds)**: Removed `sqlite3` (CGO) dependency in favor of a faster, fully static Go build process.
- **Linker Performance**: Optimized build scripts (`Makefile`, `Dockerfile`, `.deploy`) to ensure `CGO_ENABLED=0` for significantly faster incremental builds on dev machines.
- **Standardized Build**: Updated all build procedures to consistently use the optimized flags across local and container environments.

---

# Release Notes - v1.3.0 (Old)

## 📧 What's New: Gmail Integration & Better UX

### 🚀 Gmail as a New Message Source
- **Automated Email Scanning**: Connect your company Gmail to automatically scan for task-related emails.
- **AI-Powered Inbox Analysis**: Uses Gemini Pro to extract tasks from plain-text email bodies, identifying requesters and due dates just like Slack and WhatsApp messages.
- **Secure Token Management**: Each user can securely connect their Gmail account via OAuth 2.0 (`gmail.readonly` scope). Tokens are stored safely in the database and auto-refreshed as needed.

### 🎨 Improved Connection UX
- **Icon-Driven Connectivity**: You can now connect your WhatsApp or Gmail account by simply clicking their respective icons in the header when they are in the "OFF" state.
- **Visual Feedback**: Dashboard header icons now use standard colors for "Connected" and grayscale for "Disconnected," with interactive status tooltips.

---

# Release Notes - v1.2.0 (Stable)

## 🚀 What's New

### 👥 Multi-User & Multi-Session Support
- **Google Login (OAuth 2.0)**: Individual accounts now have their own secure sessions.
- **WhatsApp Multi-Client**: Multiple users can connect their own WhatsApp accounts simultaneously. The system uses session persistence to keep you logged in.
- **Improved UI Layout**: Switched to a modern tab-based design ('My Tasks' vs 'Other Tasks') for better task management.

### ⚡ Performance & Reliability
- **Neon DB Optimization**: Implemented in-memory caching and optimized connection pooling to allow the database to sleep during idle periods, saving resources.
- **Log Rotation**: Integrated `lumberjack.v2` for daily log rotation and automatic 7-day retention to prevent disk overflow on the VPS.
- **Dockerized Deployment**: Fully containerized setup for easy scaling and deployment on any VPS.

### 🛠 Fixes & Refinements
- **UI Polish**: Updated to a sleek dark theme with glassmorphism effects.
- **Soft Delete**: Tasks are now archived rather than permanently deleted, allowing for recovery and export.
- **Error Handling**: Improved backend error reporting with transparent JSON responses.

---
*Note: The LINE Messenger integration was attempted but rolled back due to API/Thrift client compatibility issues on the current infrastructure. Future attempts may be considered with updated libraries.*

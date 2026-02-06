# VideoCMS API Documentation

This documentation outlines the available API endpoints for the VideoCMS application.

## Authentication

### Login
*   **Method:** `POST`
*   **Path:** `/auth/login`
*   **Description:** Authenticates a user and returns a JWT token.
*   **Auth Required:** No
*   **Rate Limit:** Auth specific (strictly limited)
*   **Request Body (JSON/Form):**
    *   `username` (string, required, min=3, max=32)
    *   `password` (string, required, min=8, max=250)
*   **Response (JSON):**
    ```json
    {
      "token": "eyJhbGciOiJIUzI1NiIsInR...",
      "exp": "2023-10-27T10:00:00Z"
    }
    ```

### Check Token
*   **Method:** `GET`
*   **Path:** `/auth/check`
*   **Description:** Verifies the validity of the provided JWT token.
*   **Auth Required:** Yes (Bearer Token)
*   **Response (JSON):**
    ```json
    {
      "username": "user1",
      "exp": "2023-10-27T10:00:00Z"
    }
    ```

### Refresh Token
*   **Method:** `GET`
*   **Path:** `/auth/refresh`
*   **Description:** Refreshes the current JWT token.
*   **Auth Required:** Yes (Bearer Token)
*   **Response (JSON):**
    ```json
    {
      "token": "eyJhbGciOiJIUzI1NiIsInR...",
      "exp": "2023-10-27T10:00:00Z"
    }
    ```

## Folders

### Create Folder
*   **Method:** `POST`
*   **Path:** `/folder`
*   **Auth Required:** Yes
*   **Request Body (JSON/Form):**
    *   `Name` (string, required, min=1, max=120)
    *   `ParentFolderID` (number, optional)
*   **Response (JSON):**
    ```json
    {
      "ID": 5,
      "CreatedAt": "2023-10-26T10:00:00Z",
      "UpdatedAt": "2023-10-26T10:00:00Z",
      "DeletedAt": null,
      "Name": "My Folder",
      "UserID": 1,
      "ParentFolderID": 0
    }
    ```

### Update Folder
*   **Method:** `PUT`
*   **Path:** `/folder`
*   **Description:** Updates folder details (name only). To move a folder, use the `/move` endpoint.
*   **Auth Required:** Yes
*   **Request Body (JSON/Form):**
    *   `FolderID` (number, required)
    *   `Name` (string, required, min=1, max=120)
*   **Response (JSON):**
    ```json
    {
      "ID": 5,
      "Name": "Updated Folder Name",
      ...
    }
    ```

### Delete Folder
*   **Method:** `DELETE`
*   **Path:** `/folder`
*   **Auth Required:** Yes
*   **Request Body (JSON/Form):**
    *   `FolderID` (number, required)
*   **Response (String):**
    ```
    "ok"
    ```
    *(Or HTTP 204 No Content)*

### List Folders
*   **Method:** `GET`
*   **Path:** `/folders`
*   **Auth Required:** Yes
*   **Query Parameters:**
    *   `ParentFolderID` (number, optional)
    *   `UserID` (number, optional, Admin only)
*   **Response (JSON):**
    ```json
    [
      {
        "ID": 1,
        "Name": "Folder 1",
        "ParentFolderID": 0
      },
      {
        "ID": 2,
        "Name": "Folder 2",
        "ParentFolderID": 0
      }
    ]
    ```

### Move Items
*   **Method:** `PUT`
*   **Path:** `/move`
*   **Description:** Moves folders or files to a new parent folder.
*   **Auth Required:** Yes
*   **Request Body (JSON):**
    *   `ParentFolderID` (number, optional)
    *   `FolderIDs` (array of numbers, optional)
    *   `LinkIDs` (array of numbers, optional)
*   **Response (String):**
    ```
    "ok"
    ```

## Files

### Create File (Upload)
*   **Method:** `POST`
*   **Path:** `/file`
*   **Auth Required:** Yes
*   **Request Body (Multipart/Form-Data):**
    *   `file` (file, required)
    *   `Name` (string, required)
    *   `ParentFolderID` (number, optional)
*   **Response (JSON):**
    ```json
    {
      "ID": 10,
      "UUID": "550e8400-e29b-41d4-a716-446655440000",
      "Name": "video.mp4",
      "ParentFolderID": 0,
      ...
    }
    ```

### Create File (Clone)
*   **Method:** `POST`
*   **Path:** `/file/clone`
*   **Description:** Clones an existing file by its hash.
*   **Auth Required:** Yes
*   **Request Body (JSON/Form):**
    *   `Name` (string, required)
    *   `Sha256` (string, required)
    *   `ParentFolderID` (number, optional)
*   **Response (JSON):**
    ```json
    {
      "ID": 11,
      "Name": "cloned_video.mp4",
      ...
    }
    ```

### Get File Info
*   **Method:** `GET`
*   **Path:** `/file`
*   **Auth Required:** Yes
*   **Query Parameters:**
    *   `LinkID` (number, required)
*   **Response (JSON):**
    ```json
    {
      "ID": 10,
      "Name": "video.mp4",
      "File": {
          "Size": 1024000,
          "Duration": 120.5,
          "Width": 1920,
          "Height": 1080
      },
      ...
    }
    ```

### List Files
*   **Method:** `GET`
*   **Path:** `/files`
*   **Auth Required:** Yes
*   **Query Parameters:**
    *   `ParentFolderID` (number, optional)
    *   `UserID` (number, optional, Admin only)
*   **Response (JSON):**
    ```json
    [
      {
        "ID": 10,
        "Name": "video.mp4"
      },
      {
        "ID": 11,
        "Name": "another.mov"
      }
    ]
    ```

### Search Files
*   **Method:** `GET`
*   **Path:** `/files/search`
*   **Auth Required:** Yes
*   **Query Parameters:**
    *   `Query` (string, required, min=1, max=120)
    *   `UserID` (number, optional)
*   **Response (JSON):**
    ```json
    [
       {
          "ID": 10,
          "Name": "vacation_video.mp4"
       }
    ]
    ```

### Update File
*   **Method:** `PUT`
*   **Path:** `/file`
*   **Description:** Updates file details (name only). To move a file, use the `/move` endpoint.
*   **Auth Required:** Yes
*   **Request Body (JSON/Form):**
    *   `LinkID` (number, required)
    *   `Name` (string, required)
*   **Response (JSON):**
    ```json
    {
      "ID": 10,
      "Name": "updated_name.mp4"
    }
    ```

### Delete File
*   **Method:** `DELETE`
*   **Path:** `/file`
*   **Auth Required:** Yes
*   **Request Body (JSON/Form):**
    *   `LinkID` (number, required)
*   **Response (String):**
    ```
    "ok"
    ```

### Delete Multiple Files
*   **Method:** `DELETE`
*   **Path:** `/files`
*   **Auth Required:** Yes
*   **Request Body (JSON):**
    *   `LinkIDs` (array of objects: `{"LinkID": 123}`)
*   **Response (String):**
    ```
    "ok"
    ```

## Tagging

### Add Tag
*   **Method:** `POST`
*   **Path:** `/file/tag`
*   **Auth Required:** Yes
*   **Request Body (JSON/Form):**
    *   `Name` (string, required)
    *   `LinkId` (number, required)
*   **Response (JSON):**
    ```json
    {
      "ID": 1,
      "Name": "Holiday",
      "UserId": 1
    }
    ```

### Remove Tag
*   **Method:** `DELETE`
*   **Path:** `/file/tag`
*   **Auth Required:** Yes
*   **Request Body (JSON/Form):**
    *   `TagId` (number, required)
    *   `LinkId` (number, required)
*   **Response:** HTTP 204 No Content

## User Account Stats

### Traffic Stats
*   **Method:** `GET`
*   **Path:** `/account/traffic`
*   **Auth Required:** Yes
*   **Query Parameters:** `from` (RFC3339), `to` (RFC3339), `points`, `file_id`, `quality_id`
*   **Response (JSON):**
    ```json
    {
      "Traffic": [
        {"Timestamp": 1698300000000, "Bytes": 5000},
        {"Timestamp": 1698303600000, "Bytes": 12000}
      ]
    }
    ```

### Top Traffic Files
*   **Method:** `GET`
*   **Path:** `/account/traffic/top`
*   **Auth Required:** Yes
*   **Response (JSON):**
    ```json
    [
      {"ID": 10, "Name": "popular.mp4", "Value": 1024000000}
    ]
    ```

### Top Upload Files
*   **Method:** `GET`
*   **Path:** `/account/upload/top`
*   **Description:** Returns top uploaded files or users.
*   **Auth Required:** Yes
*   **Query Parameters:**
    *   `mode` (string, optional, default="files"): "files" or "users"
    *   `from` (RFC3339, optional)
    *   `to` (RFC3339, optional)
*   **Response (JSON):**
    ```json
    [
      {"ID": 10, "Name": "heavy_upload.mp4", "Value": 5000000000}
    ]
    ```

### Storage Stats
*   **Method:** `GET`
*   **Path:** `/account/storage/top`
*   **Auth Required:** Yes
*   **Response (JSON):**
    ```json
    [
       {"ID": 12, "Name": "backup.zip", "Value": 5000000000}
    ]
    ```

## Remote Downloads

### Create Remote Download
*   **Method:** `POST`
*   **Path:** `/remote/download`
*   **Auth Required:** Yes
*   **Request Body (JSON):**
    *   `urls` (array of strings, required, valid URLs)
    *   `parentFolderID` (number, optional)
*   **Response (JSON):**
    ```json
    [
      {
        "ID": 1,
        "Url": "https://example.com/video.mp4",
        "Status": "pending",
        ...
      }
    ]
    ```

### List Remote Downloads
*   **Method:** `GET`
*   **Path:** `/remote/downloads`
*   **Auth Required:** Yes
*   **Response (JSON):**
    ```json
    [
      {
        "ID": 1,
        "Url": "https://example.com/video.mp4",
        "Status": "downloading",
        "Progress": 0.45,
        "BytesDownloaded": 47185920,
        "TotalSize": 104857600,
        ...
      }
    ]
    ```

### Remote Download Traffic Stats
*   **Method:** `GET`
*   **Path:** `/account/remote-download`
*   **Auth Required:** Yes
*   **Query Parameters:** `from` (RFC3339, optional), `to` (RFC3339, optional), `points` (optional)
*   **Response (JSON):**
    ```json
    {
      "Traffic": [
        {"Timestamp": 1698300000000, "Bytes": 500000},
        ...
      ]
    }
    ```

### Remote Download Duration Stats
*   **Method:** `GET`
*   **Path:** `/account/remote-download/duration`
*   **Auth Required:** Yes
*   **Query Parameters:** `from` (RFC3339, optional), `to` (RFC3339, optional), `points` (optional)
*   **Response (JSON):**
    ```json
    {
      "Traffic": [
        {"Timestamp": 1698300000000, "Bytes": 300},
        ...
      ]
    }
    ```

### Top Remote Download Traffic
*   **Method:** `GET`
*   **Path:** `/account/remote-download/top`
*   **Auth Required:** Yes
*   **Query Parameters:**
    *   `from` (RFC3339, optional)
    *   `to` (RFC3339, optional)
    *   `mode` (string, optional, default="domains"): "domains", "users", "duration"
*   **Response (JSON):**
    ```json
    [
      {"ID": 0, "Name": "example.com", "Value": 1024000000}
    ]
    ```

## Web Pages (CMS)

### List Public Pages
*   **Method:** `GET`
*   **Path:** `/p/pages`
*   **Auth Required:** No
*   **Response (JSON):**
    ```json
    [
      {
        "Path": "about",
        "Title": "About Us",
        "ListInFooter": true
      }
    ]
    ```

### Get Public Page
*   **Method:** `GET`
*   **Path:** `/p/page`
*   **Auth Required:** No
*   **Query Parameters:** `Path` (string)
*   **Response (JSON):**
    ```json
    {
      "Path": "about",
      "Title": "About Us",
      "Html": "<h1>About</h1><p>...</p>",
      "ListInFooter": true
    }
    ```

### Create Page (Admin)
*   **Method:** `POST`
*   **Path:** `/page`
*   **Auth Required:** Yes (Admin)
*   **Request Body (JSON):**
    *   `Path`, `Title`, `Html`, `ListInFooter`
*   **Response (String):**
    ```
    "ok"
    ```

## Webhooks

### List Webhooks
*   **Method:** `GET`
*   **Path:** `/webhooks`
*   **Auth Required:** Yes
*   **Response (JSON):**
    ```json
    [
      {
        "ID": 1,
        "Name": "Discord Notifier",
        "Url": "https://discord.com/api/webhooks/...",
        "Rpm": 60
      }
    ]
    ```

### Create Webhook
*   **Method:** `POST`
*   **Path:** `/webhook`
*   **Auth Required:** Yes
*   **Request Body (JSON):**
    *   `Name`, `Url`, `Rpm`, `ReqQuery`, `ResField`
*   **Response (String):**
    ```
    "ok"
    ```

## Admin

### System Stats
*   **Method:** `GET`
*   **Path:** `/stats`
*   **Auth Required:** Yes (Admin)
*   **Response (JSON):**
    ```json
    {
      "Cpu": [{"Timestamp": 1698300000, "Value": 45.5}],
      "Mem": [{"Timestamp": 1698300000, "Value": 60.2}],
      "NetOut": [], "NetIn": [], "DiskW": [], "DiskR": []
    }
    ```

### Manage Users (List)
*   **Method:** `GET`
*   **Path:** `/users`
*   **Auth Required:** Yes (Admin)
*   **Query Parameters:** `page`, `limit`, `search`
*   **Response (JSON):**
    ```json
    {
      "users": [
        {"ID": 1, "Username": "admin", "Email": "admin@example.com", "Admin": true}
      ],
      "total": 100,
      "page": 1
    }
    ```

### Create User
*   **Method:** `POST`
*   **Path:** `/users`
*   **Auth Required:** Yes (Admin)
*   **Request Body (JSON):**
    *   `username`, `password`, `email`, `admin`, `storage`, `balance`
*   **Response (JSON):**
    ```json
    {
      "ID": 5,
      "Username": "newuser",
      "Email": "new@example.com",
      "Admin": false
    }
    ```

### Settings (Get)
*   **Method:** `GET`
*   **Path:** `/settings`
*   **Auth Required:** Yes (Admin)
*   **Response (JSON):**
    ```json
    {
      "ID": 1,
      "AppName": "VideoCMS",
      "UploadEnabled": true,
      ...
    }
    ```

### Settings (Update)
*   **Method:** `PUT`
*   **Path:** `/settings`
*   **Auth Required:** Yes (Admin)
*   **Request Body (JSON):**
    *   Full `Setting` object structure
*   **Response (String):**
    ```
    "ok"
    ```
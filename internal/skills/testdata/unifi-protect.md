---
name: unifi-protect
description: UniFi Protect camera operations with built-in routing and shared auth
secrets:
  - key: UNIFI_PROTECT_TOKEN
    description: Bearer token for UniFi Protect API
authenticationProfiles:
  protect:
    type: bearer
    token: "{{secret:UNIFI_PROTECT_TOKEN}}"
tools:
  - name: protect_ops
    description: Run one UniFi Protect action (list/get/snapshot/light/recording/privacy)
    type: workflow
    actionField: action
    parameters:
      type: object
      properties:
        action:
          type: string
          enum:
            - list_cameras
            - get_camera
            - get_snapshot
            - set_status_light
            - set_recording_mode
            - set_privacy_mode
          description: Protect operation to run
        host:
          type: string
          description: UniFi OS host (e.g. nvr.local)
        cameraId:
          type: string
          description: Camera ID for camera-specific actions
        enabled:
          type: boolean
          description: Used by set_status_light / set_privacy_mode
        mode:
          type: string
          enum: ["always", "detections", "never"]
          description: Used by set_recording_mode
        isDoorbell:
          type: boolean
          description: Filter list_cameras to doorbells (true) or non-doorbells (false)
      required: ["action", "host"]
    actions:
      list_cameras:
        - name: list_cameras
          type: http
          method: GET
          url: "https://{{host}}/proxy/protect/api/cameras"
          auth: protect
          headers:
            Accept: application/json
          result: json
      get_camera:
        - name: get_camera
          type: http
          method: GET
          url: "https://{{host}}/proxy/protect/api/cameras/{{cameraId}}"
          auth: protect
          headers:
            Accept: application/json
          result: json
      get_snapshot:
        - name: get_snapshot
          type: http
          method: GET
          url: "https://{{host}}/proxy/protect/api/cameras/{{cameraId}}/snapshot"
          auth: protect
          headers:
            Accept: image/jpeg
      set_status_light:
        - name: set_status_light
          type: http
          method: PATCH
          url: "https://{{host}}/proxy/protect/api/cameras/{{cameraId}}"
          auth: protect
          headers:
            Accept: application/json
            Content-Type: application/json
          body: '{"ledSettings":{"isEnabled":{{enabled|json}}}}'
          result: json
      set_recording_mode:
        - name: set_recording_mode
          type: http
          method: PATCH
          url: "https://{{host}}/proxy/protect/api/cameras/{{cameraId}}"
          auth: protect
          headers:
            Accept: application/json
            Content-Type: application/json
          body: '{"recordingSettings":{"mode":"{{mode}}"}}'
          result: json
      set_privacy_mode:
        - name: enable_privacy
          if: "enabled == true"
          type: http
          method: PATCH
          url: "https://{{host}}/proxy/protect/api/cameras/{{cameraId}}"
          auth: protect
          headers:
            Accept: application/json
            Content-Type: application/json
          body: '{"privacyZones":[{"id":0,"name":"Privacy","color":"#85EFAC","points":[[0,0],[1,0],[1,1],[0,1]]}]}'
          result: json
        - name: disable_privacy
          if: "enabled == false"
          type: http
          method: PATCH
          url: "https://{{host}}/proxy/protect/api/cameras/{{cameraId}}"
          auth: protect
          headers:
            Accept: application/json
            Content-Type: application/json
          body: '{"privacyZones":[]}'
          result: json
---

Use protect_ops as a single entrypoint for UniFi Protect operations.
Set `UNIFI_PROTECT_TOKEN` in TeaNode secrets settings and pass `host`.
For camera-specific actions, call list_cameras first to obtain a valid camera ID.

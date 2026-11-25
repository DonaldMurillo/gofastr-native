package dev.gofastr.nativeui.arcore;

import android.opengl.GLES11Ext;
import android.opengl.GLES20;
import android.opengl.GLSurfaceView;
import android.opengl.Matrix;
import com.google.ar.core.Anchor;
import com.google.ar.core.Camera;
import com.google.ar.core.Coordinates2d;
import com.google.ar.core.Frame;
import com.google.ar.core.HitResult;
import com.google.ar.core.Plane;
import com.google.ar.core.Session;
import com.google.ar.core.TrackingState;
import java.nio.Buffer;
import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.nio.FloatBuffer;
import javax.microedition.khronos.egl.EGLConfig;
import javax.microedition.khronos.opengles.GL10;

final class ARCameraRenderer implements GLSurfaceView.Renderer {
    private static final int EXTERNAL = GLES11Ext.GL_TEXTURE_EXTERNAL_OES;
    private static final String CAMERA_VERTEX =
        "attribute vec2 p;attribute vec2 uv;varying vec2 tex;void main(){gl_Position=vec4(p,0.,1.);tex=uv;}";
    private static final String CAMERA_FRAGMENT =
        "#extension GL_OES_EGL_image_external : require\nprecision mediump float;" +
        "uniform samplerExternalOES image;varying vec2 tex;void main(){gl_FragColor=texture2D(image,tex);}";
    private static final String POINT_VERTEX =
        "attribute vec4 p;void main(){gl_Position=p;gl_PointSize=42.;}";
    private static final String POINT_FRAGMENT =
        "precision mediump float;void main(){float r=distance(gl_PointCoord,vec2(.5));" +
        "if(r>.5)discard;gl_FragColor=r>.38?vec4(1.,1.,1.,1.):vec4(.13,.79,.65,1.);}";

    private final ARSessionActivity host;
    private final FloatBuffer quad = buffer(8).put(new float[]{-1, -1, 1, -1, -1, 1, 1, 1});
    private final FloatBuffer uv = buffer(8);
    private final float[] view = new float[16], projection = new float[16];
    private final float[] world = new float[4], eye = new float[4], clip = new float[4];
    private int texture, background, vertex, texCoord, point, pointVertex;
    private int width, height, previousWidth, previousHeight, previousRotation = -1;
    private Anchor anchor;
    private float[] tap;
    private boolean failed;

    ARCameraRenderer(ARSessionActivity host) {
        this.host = host;
    }

    void place(float x, float y) { tap = new float[]{x, y}; }

    @Override public void onSurfaceCreated(GL10 gl, EGLConfig config) {
        try {
            int[] textures = new int[1];
            GLES20.glGenTextures(1, textures, 0);
            texture = textures[0];
            GLES20.glBindTexture(EXTERNAL, texture);
            GLES20.glTexParameteri(EXTERNAL, GLES20.GL_TEXTURE_MIN_FILTER, GLES20.GL_LINEAR);
            GLES20.glTexParameteri(EXTERNAL, GLES20.GL_TEXTURE_MAG_FILTER, GLES20.GL_LINEAR);
            GLES20.glTexParameteri(EXTERNAL, GLES20.GL_TEXTURE_WRAP_S, GLES20.GL_CLAMP_TO_EDGE);
            GLES20.glTexParameteri(EXTERNAL, GLES20.GL_TEXTURE_WRAP_T, GLES20.GL_CLAMP_TO_EDGE);
            background = program(CAMERA_VERTEX, CAMERA_FRAGMENT);
            vertex = GLES20.glGetAttribLocation(background, "p");
            texCoord = GLES20.glGetAttribLocation(background, "uv");
            point = program(POINT_VERTEX, POINT_FRAGMENT);
            pointVertex = GLES20.glGetAttribLocation(point, "p");
            previousRotation = -1;
        } catch (RuntimeException e) {
            fail(e);
        }
    }

    @Override public void onSurfaceChanged(GL10 gl, int width, int height) {
        this.width = width;
        this.height = height;
        GLES20.glViewport(0, 0, width, height);
    }

    @Override public void onDrawFrame(GL10 gl) {
        GLES20.glClear(GLES20.GL_COLOR_BUFFER_BIT);
        Session session = host.session();
        if (failed || session == null || width == 0 || height == 0) return;
        try {
            int rotation = host.rotation();
            boolean geometryChanged = rotation != previousRotation || width != previousWidth || height != previousHeight;
            if (geometryChanged) {
                session.setDisplayGeometry(rotation, width, height);
                previousRotation = rotation;
                previousWidth = width;
                previousHeight = height;
            }
            session.setCameraTextureName(texture);
            Frame frame = session.update();
            if (geometryChanged || frame.hasDisplayGeometryChanged()) {
                ((Buffer)quad).rewind();
                ((Buffer)uv).rewind();
                frame.transformCoordinates2d(Coordinates2d.OPENGL_NORMALIZED_DEVICE_COORDINATES, quad,
                    Coordinates2d.TEXTURE_NORMALIZED, uv);
            }
            if (frame.getTimestamp() != 0) drawCamera();
            Camera camera = frame.getCamera();
            if (camera.getTrackingState() != TrackingState.TRACKING) {
                tap = null;
                host.updatePlacement("Move the camera slowly to start tracking.", null);
                return;
            }
            if (tap != null) {
                float[] location = tap;
                tap = null;
                for (HitResult hit : frame.hitTest(location[0], location[1])) {
                    if (hit.getTrackable() instanceof Plane) {
                        Plane plane = (Plane)hit.getTrackable();
                        if (plane.getTrackingState() == TrackingState.TRACKING && plane.isPoseInPolygon(hit.getHitPose())) {
                            Anchor placed = hit.createAnchor();
                            if (anchor != null) anchor.detach();
                            anchor = placed;
                            break;
                        }
                    }
                }
            }
            if (anchor != null && anchor.getTrackingState() == TrackingState.TRACKING) {
                float[] position = anchor.getPose().getTranslation();
                drawAnchor(camera, position);
                host.updatePlacement("Anchor placed. Tap another surface to move it, or choose Done.", position);
            } else {
                boolean found = false;
                for (Plane plane : session.getAllTrackables(Plane.class)) {
                    if (plane.getTrackingState() == TrackingState.TRACKING && plane.getSubsumedBy() == null) {
                        found = true;
                        break;
                    }
                }
                host.updatePlacement(found ? "Surface detected. Tap it to place an anchor." : "Look around for a flat surface.", null);
            }
        } catch (Exception e) {
            fail(e);
        }
    }

    private void fail(Exception e) {
        failed = true;
        host.fail(e);
    }

    private void drawCamera() {
        GLES20.glUseProgram(background);
        GLES20.glActiveTexture(GLES20.GL_TEXTURE0);
        GLES20.glBindTexture(EXTERNAL, texture);
        GLES20.glUniform1i(GLES20.glGetUniformLocation(background, "image"), 0);
        ((Buffer)quad).rewind();
        ((Buffer)uv).rewind();
        GLES20.glVertexAttribPointer(vertex, 2, GLES20.GL_FLOAT, false, 0, quad);
        GLES20.glVertexAttribPointer(texCoord, 2, GLES20.GL_FLOAT, false, 0, uv);
        GLES20.glEnableVertexAttribArray(vertex);
        GLES20.glEnableVertexAttribArray(texCoord);
        GLES20.glDrawArrays(GLES20.GL_TRIANGLE_STRIP, 0, 4);
        GLES20.glDisableVertexAttribArray(vertex);
        GLES20.glDisableVertexAttribArray(texCoord);
    }

    private void drawAnchor(Camera camera, float[] position) {
        camera.getViewMatrix(view, 0);
        camera.getProjectionMatrix(projection, 0, .03f, 100f);
        System.arraycopy(position, 0, world, 0, 3);
        world[3] = 1;
        Matrix.multiplyMV(eye, 0, view, 0, world, 0);
        Matrix.multiplyMV(clip, 0, projection, 0, eye, 0);
        if (clip[3] <= 0) return;
        GLES20.glUseProgram(point);
        GLES20.glVertexAttrib4fv(pointVertex, clip, 0);
        GLES20.glDrawArrays(GLES20.GL_POINTS, 0, 1);
    }

    private static FloatBuffer buffer(int size) {
        return ByteBuffer.allocateDirect(size * Float.BYTES).order(ByteOrder.nativeOrder()).asFloatBuffer();
    }

    private static int program(String vertex, String fragment) {
        int v = shader(GLES20.GL_VERTEX_SHADER, vertex), f = shader(GLES20.GL_FRAGMENT_SHADER, fragment);
        int program = GLES20.glCreateProgram();
        GLES20.glAttachShader(program, v);
        GLES20.glAttachShader(program, f);
        GLES20.glLinkProgram(program);
        GLES20.glDeleteShader(v);
        GLES20.glDeleteShader(f);
        int[] linked = new int[1];
        GLES20.glGetProgramiv(program, GLES20.GL_LINK_STATUS, linked, 0);
        if (linked[0] == 0) throw new IllegalStateException(GLES20.glGetProgramInfoLog(program));
        return program;
    }

    private static int shader(int kind, String source) {
        int shader = GLES20.glCreateShader(kind);
        GLES20.glShaderSource(shader, source);
        GLES20.glCompileShader(shader);
        int[] compiled = new int[1];
        GLES20.glGetShaderiv(shader, GLES20.GL_COMPILE_STATUS, compiled, 0);
        if (compiled[0] == 0) throw new IllegalStateException(GLES20.glGetShaderInfoLog(shader));
        return shader;
    }
}

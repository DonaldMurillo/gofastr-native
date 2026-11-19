package dev.gofastr.nativeui;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;

/** Bounded stream reads for device-service payloads. */
public final class Bounded {
    private Bounded() {}

    /** Reads the stream fully, throwing the limit message once more than maximum bytes arrive. */
    public static byte[] read(InputStream in,int maximum,String limit)throws IOException {
        try(InputStream input=in;ByteArrayOutputStream out=new ByteArrayOutputStream()) {
            byte[] buffer=new byte[16384];int count;
            while((count=input.read(buffer))!=-1){if(out.size()+count>maximum)throw new IOException(limit);out.write(buffer,0,count);}
            return out.toByteArray();
        }
    }
}

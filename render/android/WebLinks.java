package dev.gofastr.nativeui;

import java.net.URI;
import java.net.URISyntaxException;
import java.util.List;

final class WebLinks {
    private WebLinks() {}

    static URI parse(String value) {
        if (value.length() > 4096) return null;
        try {
            URI uri = new URI(value);
            if (!"https".equalsIgnoreCase(uri.getScheme()) && !"http".equalsIgnoreCase(uri.getScheme())) return null;
            if (uri.getHost() == null || uri.getRawUserInfo() != null || uri.getRawAuthority().endsWith(":")) return null;
            if (uri.getPort() == 0 || uri.getPort() > 65535) return null;
            return uri;
        } catch (URISyntaxException e) {
            return null;
        }
    }

    static String route(String value, List<String> hosts) {
        if (hosts.isEmpty()) return "";
        URI uri = parse(value);
        if (uri == null || !hosts.contains(uri.getHost())) return "";
        int defaultPort = "https".equalsIgnoreCase(uri.getScheme()) ? 443 : 80;
        if (uri.getPort() != -1 && uri.getPort() != defaultPort) return "";
        String authority = uri.getHost();
        if (uri.getPort() != -1) authority += ":" + defaultPort;
        if (!authority.equals(uri.getRawAuthority())) return "";
        String path = uri.getRawPath().isEmpty() ? "/" : uri.getRawPath();
        return uri.getRawQuery() == null ? path : path + "?" + uri.getRawQuery();
    }
}

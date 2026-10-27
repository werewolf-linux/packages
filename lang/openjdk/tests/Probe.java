// What this JVM lets a program do, as one JSON line; tests/probe.sh compares
// it with what the protections in patches/series and the image's allowances say.
import com.sun.management.HotSpotDiagnosticMXBean;
import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InvalidClassException;
import java.io.ObjectInputStream;
import java.io.ObjectOutputStream;
import java.lang.foreign.MemorySegment;
import java.lang.management.ManagementFactory;
import java.lang.reflect.Field;

public class Probe {
    public static void main(String[] args) throws Exception {
        if (args.length > 0 && args[0].equals("hide-stderr")) {
            System.setErr(new java.io.PrintStream(java.io.OutputStream.nullOutputStream()));
        }
        HotSpotDiagnosticMXBean vm = ManagementFactory.getPlatformMXBean(HotSpotDiagnosticMXBean.class);
        String attach = vm.getVMOption("DisableAttachMechanism").getValue();
        String perfData = vm.getVMOption("UsePerfData").getValue();
        System.out.println("{\"options\":\"" + System.getProperty("werewolf.probe", "none")
                + "\",\"attach\":\"" + ("true".equals(attach) ? "disabled" : "enabled")
                + "\",\"perf_data\":\"" + ("true".equals(perfData) ? "on" : "off")
                + "\",\"deserialize\":\"" + deserialize(Integer.valueOf(7))
                + "\",\"primitive_array\":\"" + deserialize(new int[] {7})
                + "\",\"unsafe\":\"" + unsafe()
                + "\",\"unmap\":\"" + unmap()
                + "\",\"native\":\"" + nativeAccess()
                + "\",\"exec\":\"" + exec()
                + "\",\"xml_external\":\"" + xmlExternal() + "\"}");
    }

    static String deserialize(Object value) throws IOException {
        ByteArrayOutputStream bytes = new ByteArrayOutputStream();
        try (ObjectOutputStream out = new ObjectOutputStream(bytes)) {
            out.writeObject(value);
        }
        try (ObjectInputStream in = new ObjectInputStream(new ByteArrayInputStream(bytes.toByteArray()))) {
            in.readObject();
            return "read";
        } catch (InvalidClassException e) {
            return "refused";
        } catch (ClassNotFoundException e) {
            return "missing";
        }
    }

    static String unsafe() throws Exception {
        Field field = sun.misc.Unsafe.class.getDeclaredField("theUnsafe");
        field.setAccessible(true);
        sun.misc.Unsafe unsafe = (sun.misc.Unsafe) field.get(null);
        try {
            unsafe.freeMemory(unsafe.allocateMemory(8));
            return "allowed";
        } catch (UnsupportedOperationException e) {
            return "refused";
        }
    }

    // Freeing a direct buffer touches none of its memory: java:unmap's alone.
    static String unmap() throws Exception {
        Field field = sun.misc.Unsafe.class.getDeclaredField("theUnsafe");
        field.setAccessible(true);
        sun.misc.Unsafe unsafe = (sun.misc.Unsafe) field.get(null);
        try {
            unsafe.invokeCleaner(java.nio.ByteBuffer.allocateDirect(8));
            return "allowed";
        } catch (UnsupportedOperationException e) {
            return "refused";
        }
    }

    static String exec() {
        try {
            new ProcessBuilder("true").start().waitFor();
            return "allowed";
        } catch (IOException | InterruptedException e) {
            return "refused";
        }
    }

    // An external entity naming a file: read, or refused unresolved.
    static String xmlExternal() throws IOException {
        java.nio.file.Path entity = java.nio.file.Path.of("xml-entity.txt").toAbsolutePath();
        java.nio.file.Files.writeString(entity, "werewolf");
        String file = entity.toUri().toString();
        String doc = "<!DOCTYPE r [<!ENTITY e SYSTEM \"" + file + "\">]><r>&e;</r>";
        try {
            javax.xml.parsers.DocumentBuilderFactory.newInstance().newDocumentBuilder()
                    .parse(new ByteArrayInputStream(doc.getBytes()));
            return "read";
        } catch (Exception e) {
            return "refused";
        }
    }

    static String nativeAccess() {
        try {
            MemorySegment.NULL.reinterpret(1);  // a restricted method: native access
            return "allowed";
        } catch (IllegalCallerException e) {
            return "refused";
        }
    }
}

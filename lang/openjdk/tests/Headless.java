import java.awt.Color;
import java.awt.Font;
import java.awt.Graphics2D;
import java.awt.GraphicsEnvironment;
import java.awt.image.BufferedImage;
import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import javax.imageio.ImageIO;

// Server consumers still render QR codes, charts and documents without X11.
public class Headless {
    public static void main(String[] args) throws Exception {
        if (!GraphicsEnvironment.isHeadless()) throw new AssertionError("headless");
        var image = new BufferedImage(200, 80, BufferedImage.TYPE_INT_RGB);
        Graphics2D graphics = image.createGraphics();
        graphics.setColor(Color.WHITE);
        graphics.fillRect(0, 0, 200, 80);
        graphics.setColor(Color.BLACK);
        graphics.setFont(new Font("SansSerif", Font.PLAIN, 18));
        graphics.drawString("Login 123456", 5, 30);
        graphics.fillRect(5, 40, 20, 20);
        graphics.dispose();
        var bytes = new ByteArrayOutputStream();
        if (!ImageIO.write(image, "png", bytes)) throw new AssertionError("PNG encoder");
        var decoded = ImageIO.read(new ByteArrayInputStream(bytes.toByteArray()));
        if (decoded.getWidth() != 200 || decoded.getRGB(10, 45) != Color.BLACK.getRGB())
            throw new AssertionError("PNG round trip");
        System.out.println("probe: ok: headless fonts and PNG rendering");
    }
}

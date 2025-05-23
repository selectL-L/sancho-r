package funcs

import (
	"bytes"
	"fmt"
	"image/png"
	"io"
	"math"
	"net/http"
	"os"
	"strings"

	"github.com/bwmarrin/discordgo"
	"gopkg.in/gographics/imagick.v3/imagick"
)

type ImageProc struct {
	Alias string
	Func  func(*imagick.MagickWand) (*[]byte, error)
}

var imageprocs = []ImageProc{
	{"yesod", yesod},
	{"jpeg", jpeg},
	{"corru", corru},
}

func ApplyImageProcessing(inst *Instance, m *discordgo.MessageCreate) {
	c, err := inst.Session.State.Channel(m.ChannelID)
	if err != nil {
		inst.ErrorChan <- err
		return
	}
	orb := imagick.NewMagickWand()
	defer orb.Destroy()
	orb.SetOption("MAGICK_OCL_DEVICE", "GPU")
	var resp *http.Response
	var form string = "jpg"
	var targetMsg *discordgo.Message = m.Message

	if (len(m.Attachments) == 0 || !strings.Contains(m.Attachments[0].ContentType, "image")) && !strings.Contains(m.Content, "https://") {
		if m.ReferencedMessage == nil {
			iKnowWhatYouAre(inst, m)
			return
		}
		if (len(m.ReferencedMessage.Attachments) == 0 || !strings.Contains(m.ReferencedMessage.Attachments[0].ContentType, "image")) && !strings.Contains(m.ReferencedMessage.Content, "https://") {
			iKnowWhatYouAre(inst, m)
			return
		}
		targetMsg = m.ReferencedMessage
	}

	var targets []string

	if len(targetMsg.Attachments) > 0 {
		for _, att := range targetMsg.Attachments {
			targets = append(targets, att.URL)
		}
	} else {
		if strings.Contains(targetMsg.Content, "discordapp") {
			// link, _, _ := strings.Cut(targetMsg.Content[strings.Index(targetMsg.Content, "https://"):], " ")
			// resp, err = requestImageViaAPI(inst.Session, link)
			// fmt.Println(link)
			// sorry this shit still doesn't work
			inst.Session.ChannelMessageSendReply(m.ChannelID, "Unfortunately, Discord CDN link attachments are not supported. My creator tried their best.", m.SoftReference())
			return
		} else if strings.Contains(targetMsg.Content, "tenor.com") {
			var orig []byte
			link, _, _ := strings.Cut(targetMsg.Content[strings.Index(targetMsg.Content, "https://tenor.com"):], " ")
			resp, err = http.Get(link)

			if err != nil || resp.StatusCode != http.StatusOK {
				fmt.Println("couldn't get image from tenor:", resp.StatusCode, err)
				Sadness(inst, m)
				return
			}
			orig, err = io.ReadAll(resp.Body)
			if err != nil {
				fmt.Println("couldn't extract from html")
				Sadness(inst, m)
				return
			}

			st := string(orig)
			fieldStart := strings.Index(st, "contentUrl") + len("contentUrl\":\"")
			url := strings.ReplaceAll(st[fieldStart:fieldStart+strings.Index(st[fieldStart:], "\"")], "\\u002F", "/")
			//resp, err = http.Get(url)
			targets = append(targets, url)
		} else { // hope for the best
			link, _, _ := strings.Cut(targetMsg.Content[strings.Index(targetMsg.Content, "https://"):], " ")
			targets = append(targets, link)
		}
	}

	var outputFiles []*discordgo.File
	for _, src := range targets {
		resp, err = http.Get(src)

		if err != nil || resp.StatusCode != http.StatusOK {
			fmt.Println("couldn't get image from internet:", resp.StatusCode, err)
			return
		}
		orig, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			fmt.Println("couldn't extract from html")
			Sadness(inst, m)
			return
		}

		err = orb.ReadImageBlob(orig)
		if err != nil {
			fmt.Println("couldn't read img into orb", err)
			Sadness(inst, m)
			return
		}

		for _, r := range imageprocs {
			if strings.Split(m.Content[1:], " ")[0] == r.Alias {
				out, err := r.Func(orb)
				if err != nil {
					inst.ErrorChan <- err
					fmt.Println(err)
					return
				}
				outReader := bytes.NewReader(*out)
				form = orb.GetFormat()
				outputFiles = append(outputFiles, &discordgo.File{
					Name:   "img." + form,
					Reader: outReader,
				})
			}
		}
		orb.Clear()
	}

	inst.Session.ChannelMessageSendComplex(c.ID, &discordgo.MessageSend{
		Reference: m.Reference(),
		Files: outputFiles,
		})
}

func Jpegify(orb *imagick.MagickWand, quality int) (*[]byte, error) {
	var out []byte
	if orb.GetNumberImages() > 1 {
		orb = orb.CoalesceImages()
		orb.SetFormat("gif")
		var disp imagick.DisposeType
		var w, h, del uint
		var x, y int
		for i := 0; i < int(orb.GetNumberImages()); i++ {
			orb.SetIteratorIndex(i)
			del = orb.GetImageDelay()
			w, h, x, y, _ = orb.GetImagePage()
			disp = orb.GetImageDispose()
			jpegifyImg(orb, quality)
			out, _ = orb.GetImageBlob()
			orb.ReadImageBlob(out)
			if i != int(orb.GetIteratorIndex()) {
				orb.PreviousImage()
			}
			orb.RemoveImage()
			orb.SetImageFormat("gif")
			orb.SetImageDispose(disp)
			orb.SetImageDelay(del)
			orb.SetImagePage(w, h, x, y)
		}
		out, err = orb.GetImagesBlob()
	} else {
		orb.SetFormat("jpg")
		jpegifyImg(orb, quality)
		out, err = orb.GetImageBlob()
	}
	if err != nil {
		fmt.Println("couldn't shove it back in")
		return nil, err
	}

	return &out, nil
}

func jpegifyImg(orb *imagick.MagickWand, q int) {
	orb.SetImageFormat("JPEG")
	orb.SetImageCompressionQuality(uint(q))
	orb.SetCompressionQuality(uint(q))
	if q < 2 {
		x, y := orb.GetImageWidth(), orb.GetImageHeight()
		scalingFactor := math.Max(float64(x/160), float64(y/120)) // analogous to downscaling it to fit in a 240x180 box
		orb.ModulateImage(100, 150, 100)
		orb.ResizeImage(uint(float64(x)/scalingFactor), uint(float64(y)/scalingFactor), imagick.FILTER_BOX)
		out, _ := orb.GetImageBlob()
		orb.ReadImageBlob(out)
		if orb.GetIteratorIndex() == orb.GetNumberImages()-1 {
			orb.PreviousImage()
		}
		orb.RemoveImage()
		orb.ResizeImage(x, y, imagick.FILTER_BOX)
		orb.SetImageFormat("JPEG")
		orb.PosterizeImage(16, imagick.DITHER_METHOD_FLOYD_STEINBERG)
		orb.SetImageCompressionQuality(uint(q))
		orb.SetCompressionQuality(uint(q))
	}
}

func LamentMournAndDespair(i *Instance, m *discordgo.MessageCreate) {
	type frameData struct {
		f, s, x, y int
	}

	frames := []frameData{
		{16, 105, 409, 153},
		{17, 100, 418, 146},
		{18, 100, 378, 147},
		{27, 100, 249, 167},
		{28, 95, 249, 167},
		{29, 93, 249, 167},
		{30, 90, 249, 167},
		{31, 87, 249, 167},
		{32, 85, 248, 154},
		{33, 85, 245, 142},
		{34, 85, 281, 119},
		{35, 85, 270, 90},
		{38, 85, 238, 66},
		{39, 78, 263, 77},
		{40, 70, 291, 88},
		{41, 66, 294, 91},
		{42, 63, 298, 91},
		{43, 63, 300, 91},
		{51, 85, 292, 49},
		{52, 80, 298, 65},
		{53, 80, 307, 70},
		{54, 78, 313, 75},
		{55, 77, 315, 73},
		{65, 170, 438, -11},
		{66, 165, 408, 11},
		{67, 160, 404, 10},
		{68, 155, 389, 17},
		{91, 70, 393, 148},
		{92, 65, 378, 147},
		{93, 57, 375, 147},
		{94, 56, 373, 146},
		{95, 55, 371, 146},
		{96, 54, 369, 146},
		{97, 53, 367, 146},
		{98, 52, 365, 146},
		{99, 52, 364, 146},
		{100, 51, 363, 146},
		{101, 51, 362, 146},
		{102, 50, 361, 146},
		{103, 50, 360, 146},
		{104, 49, 359, 146},
		{105, 49, 358, 146},
	}

	orb := imagick.NewMagickWand()
	defer orb.Destroy()
	lmdOrig, err := os.Open("img/lmd.gif")
	if err != nil {
		Sadness(i, m)
		return
	}
	defer lmdOrig.Close()
	orb.ReadImageFile(lmdOrig)
	target := m.Author
	if m.ReferencedMessage != nil {
		target = m.ReferencedMessage.Author
	}
	if len(m.Mentions) > 0 {
		target = m.Mentions[0]
	}
	avatar, err := i.Session.UserAvatarDecode(target)
	if err != nil {
		Sadness(i, m)
		return
	}
	var buf bytes.Buffer
	err = png.Encode(&buf, avatar)
	if err != nil {
		Sadness(i, m)
		return
	}
	avatarOrb := imagick.NewMagickWand()
	defer avatarOrb.Destroy()
	err = avatarOrb.ReadImageBlob(buf.Bytes())
	if err != nil {
		Sadness(i, m)
		return
	}
	bwavatarOrb := avatarOrb.Clone()
	bwavatarOrb.SetImageAlphaChannel(imagick.ALPHA_CHANNEL_DEACTIVATE)
	// bwavatarOrb.ModulateImage(50,0,100)
	// bwavatarOrb.SigmoidalContrastImage(true,20,100)
	// bwavatarOrb.BrightnessContrastImage(-25,80)
	avgOrb := bwavatarOrb.Clone()
	avgOrb.ResizeImage(1, 1, imagick.FILTER_BOX)
	avgOrb.ModulateImage(100, 0, 0)
	a, _ := avgOrb.GetImagePixelColor(1, 1)
	avg := a.GetBlueQuantum() //should be the same, hopefully
	bwavatarOrb.ThresholdImage(float64(avg))
	bwavatarOrb.ModulateImage(101, 100, 100)
	for _, fr := range frames {
		orb.SetIteratorIndex(fr.f)
		if fr.f > 50 && fr.f < 90 {
			bwavatarOrb.ScaleImage(uint(128*fr.s/100), uint(128*fr.s/100))
			orb.CompositeImage(bwavatarOrb, imagick.COMPOSITE_OP_OVER, true, fr.x-fr.s*64/100, fr.y-fr.s*64/100)
		} else {
			avatarOrb.ScaleImage(uint(128*fr.s/100), uint(128*fr.s/100))
			orb.CompositeImage(avatarOrb, imagick.COMPOSITE_OP_OVER, true, fr.x-fr.s*64/100, fr.y-fr.s*64/100)
		}
		orb.SetImageDispose(imagick.DISPOSE_NONE)
	}
	orb = orb.CoalesceImages()
	orb.SetImageFormat("GIF")
	out, err := orb.GetImagesBlob()
	if err != nil {
		Sadness(i, m)
		return
	}
	outReader := bytes.NewReader(out)
	i.Session.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Reference: m.Reference(),
		Files: []*discordgo.File{
			{
				Name:   "img.gif",
				Reader: outReader,
			},
		},
	})
}

func SpeechBubble(i *Instance, m *discordgo.MessageCreate) {
	orb := imagick.NewMagickWand()
	defer orb.Destroy()
	alphaOrb := imagick.NewMagickWand()
	defer alphaOrb.Destroy()

	var resp *http.Response
	if len(m.Attachments) == 0 || !strings.Contains(m.Attachments[0].ContentType, "image") {
		if m.ReferencedMessage == nil {
			i.Session.ChannelMessageSendReply(m.ChannelID, "Please send an actual image.", m.Reference())
			return
		}
		if len(m.ReferencedMessage.Attachments) == 0 || !strings.Contains(m.ReferencedMessage.Attachments[0].ContentType, "image") {
			if !strings.HasPrefix(m.ReferencedMessage.Content, "http") {
				i.Session.ChannelMessageSendReply(m.ChannelID, "Please send an actual image.", m.Reference())
				return
			} else {
				resp, err = http.Get(m.ReferencedMessage.Content)
				fmt.Println(m.ReferencedMessage.Content)
			}
		} else {
			resp, err = http.Get(m.ReferencedMessage.Attachments[0].URL)
		}
	} else {
		resp, err = http.Get(m.Attachments[0].URL)
	}

	if err != nil {
		fmt.Println("couldn't get image from internet")
		Sadness(i, m)
		return
	}
	orig, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		fmt.Println("couldn't extract from html")
		Sadness(i, m)
		return
	}

	_ = orb.ReadImageBlob(orig)
	defer orb.Clear()

	orb.SetFormat("PNGA")
	orb.SetImageFormat("PNGA")
	orb.AddImage(orb.GetImage())
	orb.SetLastIterator()
	orb.SetImageAlphaChannel(imagick.ALPHA_CHANNEL_EXTRACT)

	maskFile, err := os.Open("img/mask.png")
	if err != nil {
		Sadness(i, m)
		return
	}
	alphaOrb.ReadImageFile(maskFile)
	alphaOrb.ResizeImage(orb.GetImageWidth(), orb.GetImageHeight(), imagick.FILTER_GAUSSIAN)
	alphaOrb.SetImageAlphaChannel(imagick.ALPHA_CHANNEL_OFF)
	alphaOrb.CompositeImage(orb, imagick.COMPOSITE_OP_DARKEN, true, 0, 0)

	orb.SetFirstIterator()
	orb.SetImageAlphaChannel(imagick.ALPHA_CHANNEL_OFF)
	orb.SetFormat("PNG")
	orb.SetImageFormat("PNG")
	alphaOrb.SetFormat("PNG")
	alphaOrb.SetImageFormat("PNG")
	orb.CompositeImage(alphaOrb, imagick.COMPOSITE_OP_COPY_ALPHA, true, 0, 0)

	out, err := orb.GetImageBlob()
	if err != nil {
		Sadness(i, m)
		return
	}
	outReader := bytes.NewReader(out)
	i.Session.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Reference: m.Reference(),
		Files: []*discordgo.File{
			{
				Name:   "img.png",
				Reader: outReader,
			},
		},
	})
}

// for special use with discord links
// solution to a month-old problem generously provided by yumi
func requestImageViaAPI(s *discordgo.Session, url string) (*http.Response, error) {
	urlParts := strings.Split(url, "/")
	if len(urlParts) < 7 {
		return nil, fmt.Errorf("invalid attachment URL format")
	}

	channelID := urlParts[4]
	attachmentID := urlParts[5]

	// Create a new HTTP client with your bot's authorization
	client := &http.Client{}
	req, err := http.NewRequest("GET", fmt.Sprintf("https://discord.com/api/v9/channels/%s/messages/attachments/%s", channelID, attachmentID), nil)
	if err != nil {
		return nil, err
	}

	// Add the authorization header with your bot token
	fmt.Printf("https://discord.com/api/v9/channels/%s/messages/attachments/%s\n", channelID, attachmentID)
	req.Header.Add("Authorization", s.Token)

	// Make the request
	return client.Do(req)
}

func yesod(orb *imagick.MagickWand) (*[]byte, error) {
	return Jpegify(orb, 1)
}

func jpeg(orb *imagick.MagickWand) (*[]byte, error) {
	return Jpegify(orb, 4)
}

func corru(orb *imagick.MagickWand) (*[]byte, error) {
	var out []byte

	mapOrb := imagick.NewMagickWand()
	defer mapOrb.Destroy()

	obeskMap, err := os.Open("img/obesk.png")
	if err != nil {
		return nil, err
	}
	err = mapOrb.ReadImageFile(obeskMap)
	if err != nil {
		return nil, err
	}

	if orb.GetNumberImages() > 1 {
		orb.SetFormat("gif")
		orb = orb.CoalesceImages()
		orb.SetFormat("gif")
		var disp imagick.DisposeType
		var w, h, del uint
		var x, y int
		for i := 0; i < int(orb.GetNumberImages()); i++ {
			orb.SetIteratorIndex(i)
			del = orb.GetImageDelay()
			w, h, x, y, _ = orb.GetImagePage()
			disp = orb.GetImageDispose()
			corruifyImg(orb, mapOrb)
			out, _ = orb.GetImageBlob()
			orb.ReadImageBlob(out)
			if i != int(orb.GetIteratorIndex()) {
				orb.PreviousImage()
			}
			orb.RemoveImage()
			orb.SetImageFormat("gif")
			orb.SetImageDispose(disp)
			orb.SetImageDelay(del)
			orb.SetImagePage(w, h, x, y)
		}
		out, err = orb.GetImagesBlob()
	} else {
		orb.SetFormat("png")
		//balanceBrightness(orb, mapOrb)
		orb.AutoGammaImage()
		corruifyImg(orb, mapOrb)
		out, err = orb.GetImageBlob()
	}
	if err != nil {
		fmt.Println("couldn't shove it back in")
		return nil, err
	}

	return &out, nil
}

func corruifyImg(orb *imagick.MagickWand, remapOrb *imagick.MagickWand) {
	orb.SetImageFormat("png")
	orb.SigmoidalContrastImage(true, 10, imagick.QUANTUM_RANGE/2+1024)
	orb.PosterizeImage(12, imagick.DITHER_METHOD_NO)
	x, y := orb.GetImageWidth(), orb.GetImageHeight()
	scalingFactor := math.Min(float64(x/300), float64(y/300)) // analogous to downscaling it to fit in a 240x180 box
	orb.ResizeImage(uint(float64(x)/scalingFactor), uint(float64(y)/scalingFactor), imagick.FILTER_POINT)
	orb.RemapImage(remapOrb, imagick.DITHER_METHOD_FLOYD_STEINBERG)
}

func balanceBrightness(orb *imagick.MagickWand, remapOrb *imagick.MagickWand) {
	epsilon := 0.001
	ideal := 0.20

	brightnessOrb := imagick.NewMagickWand()
	brightnessOrb.AddImage(orb.GetImage())
	brightnessOrb.SetFirstIterator()
	corruifyImg(brightnessOrb, remapOrb)
	brightnessOrb.ResizeImage(1,1, imagick.FILTER_BOX)
	px, _ := brightnessOrb.GetImagePixelColor(1,1)
	_,_, brightnessValue := px.GetHSL()

	for i:=0; math.Abs(brightnessValue-ideal)>epsilon && i<20; i++{	
		fmt.Printf("current brightness value is %f\n", brightnessValue)
		orb.BrightnessContrastImage((100.0/float64(int(1)<<(i+1)))*((ideal-brightnessValue)/math.Abs(ideal-brightnessValue)),0)
		fmt.Printf("adjusted brightness by %f\n", (100.0/float64(int(1)<<(i+1)))*((ideal-brightnessValue)/math.Abs(ideal-brightnessValue)))
		brightnessOrb.SetImage(orb.GetImage())
		corruifyImg(brightnessOrb, remapOrb)
		brightnessOrb.ResizeImage(1,1, imagick.FILTER_BOX)
		px, _ := brightnessOrb.GetImagePixelColor(1,1)
		_,_, brightnessValue = px.GetHSL()
	}
}